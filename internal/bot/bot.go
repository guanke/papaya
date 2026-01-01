package bot

import (
	"context"
	"fmt"
    "bytes"
    "net/http"
    "io"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/guanke/papaya/internal/chat"
	"github.com/guanke/papaya/internal/config"
	"github.com/guanke/papaya/internal/r2"
	"github.com/guanke/papaya/internal/store"
)

const (
	checkInReward = 10
	chatCost      = 1
	mediaReward   = 5 // Reward for adding media? Or just free. Let's make it free for now.
)

// Bot wires together Telegram updates, persistence, and chat backend.
type Bot struct {
	api        *tgbotapi.BotAPI
	store      *store.Store
	chat       *chat.Manager
	cfg        *config.Config
	userStates sync.Map // map[int64]string (userID -> state)
    r2         *r2.Client
}

// New creates a Bot instance.
func New(cfg *config.Config, st *store.Store, manager *chat.Manager) (*Bot, error) {
	api, err := tgbotapi.NewBotAPI(cfg.BotToken)
	if err != nil {
		return nil, err
	}
    
    var r2Client *r2.Client
    if cfg.R2AccountID != "" {
        r2Client, err = r2.New(cfg.R2AccountID, cfg.R2AccessKeyID, cfg.R2SecretAccessKey, cfg.R2BucketName, cfg.R2PublicURL)
        if err != nil {
            log.Printf("failed to init R2: %v", err)
        }
    }

	return &Bot{api: api, store: st, chat: manager, cfg: cfg, r2: r2Client}, nil
}

// Run starts processing Telegram updates.
// Run starts processing Telegram updates.
func (b *Bot) Run(ctx context.Context) error {
	commands := []tgbotapi.BotCommand{
		{Command: "checkin", Description: "每日签到"},
		{Command: "points", Description: "查看积分"},
		{Command: "help", Description: "使用说明"},
		{Command: "users", Description: "[Admin] 用户列表"},
		{Command: "addpoints", Description: "[Admin] 增减积分"},
		{Command: "setpoints", Description: "[Admin] 设定积分"},
		{Command: "setmodel", Description: "[Admin] 切换模型"},
		{Command: "setadmin", Description: "[Admin] 设管理员"},
		{Command: "image", Description: "随机美图/视频"},
		{Command: "image", Description: "随机美图/视频"},
		{Command: "images", Description: "[Admin] 媒体管理"},
		{Command: "r2list", Description: "[Admin] R2文件列表"},
		{Command: "r2upload", Description: "[Admin] 上传(回复图片)"},
	}
	if _, err := b.api.Request(tgbotapi.NewSetMyCommands(commands...)); err != nil {
		log.Printf("set commands failed: %v", err)
	}

	log.Printf("Bot authorized as @%s", b.api.Self.UserName)
	updateCfg := tgbotapi.NewUpdate(0)
	updateCfg.Timeout = 30
	updates := b.api.GetUpdatesChan(updateCfg)
	for {
		select {
		case <-ctx.Done():
			return nil
		case update := <-updates:
			if update.CallbackQuery != nil {
				b.handleCallback(update.CallbackQuery)
				continue
			}
			if update.Message == nil {
				continue
			}
			b.handleMessage(update.Message)
		}
	}
}

func (b *Bot) handleMessage(msg *tgbotapi.Message) {
	userID := msg.From.ID
	username := msg.From.UserName
	displayName := strings.TrimSpace(fmt.Sprintf("%s %s", msg.From.FirstName, msg.From.LastName))
	user, err := b.store.GetOrCreateUser(userID, username, displayName)
	if err != nil {
		b.reply(msg, "无法加载用户信息，请稍后重试。")
		return
	}
	for _, id := range b.cfg.AdminIDs {
		if id == userID && !user.IsAdmin {
			if _, err := b.store.PromoteAdmin(userID); err != nil {
				log.Printf("promote admin failed: %v", err)
			}
			user.IsAdmin = true
		}
	}

	if state, ok := b.userStates.Load(userID); ok {
		s := state.(string)
		if strings.HasPrefix(s, "waiting_custom_points:") {
			b.userStates.Delete(userID)
			targetIDStr := strings.TrimPrefix(s, "waiting_custom_points:")
			targetID, _ := strconv.ParseInt(targetIDStr, 10, 64)
			points, err := strconv.Atoi(msg.Text)
			if err != nil {
				b.reply(msg, "输入无效，请输入整数。")
				return
			}
			updated, err := b.store.AddPoints(targetID, points)
			if err != nil {
				b.reply(msg, fmt.Sprintf("修改失败：%v", err))
				return
			}
			b.reply(msg, fmt.Sprintf("用户 %d 当前积分：%d", updated.ID, updated.Points))
			return
		}
	}

	if msg.IsCommand() {
		b.handleCommand(user, msg)
		return
	}

	// Handle simple media saving (Direct or Reply)
    // Check if message has media
    mediaID := ""
    mediaType := ""
    caption := msg.Caption

    if len(msg.Photo) > 0 {
        mediaID = msg.Photo[len(msg.Photo)-1].FileID
        mediaType = "photo"
    } else if msg.Video != nil {
        mediaID = msg.Video.FileID
        mediaType = "video"
    }

    // Direct message with media
    if mediaID != "" && msg.Chat.IsPrivate() {
        if err := b.store.SaveMedia(mediaID, mediaType, caption, userID); err != nil {
            log.Printf("save media failed: %v", err)
            b.reply(msg, "保存失败。")
        } else {
            b.reply(msg, "已保存到媒体库！")
        }
        return
    }

	// In group chats, only respond if mentioned
	if msg.Chat.IsGroup() || msg.Chat.IsSuperGroup() {
		if !strings.Contains(msg.Text, "@"+b.api.Self.UserName) {
			return
		}
        // If mentioned and replying to a media message
        if msg.ReplyToMessage != nil {
            reply := msg.ReplyToMessage
            if len(reply.Photo) > 0 {
                mediaID = reply.Photo[len(reply.Photo)-1].FileID
                mediaType = "photo"
                caption = reply.Caption
            } else if reply.Video != nil {
                mediaID = reply.Video.FileID
                mediaType = "video"
                caption = reply.Caption
            }

            if mediaID != "" {
                 if err := b.store.SaveMedia(mediaID, mediaType, caption, userID); err != nil {
                    log.Printf("save reply media failed: %v", err)
                    b.reply(msg, "保存失败。")
                } else {
                    b.reply(msg, "已保存引用的媒体！")
                }
                return
            }
        }
	}

	b.handleChat(user, msg)
}

func (b *Bot) handleCommand(user *store.User, msg *tgbotapi.Message) {
	switch msg.Command() {
	case "start", "help":
		msgResp := tgbotapi.NewMessage(msg.Chat.ID, b.helpText(user.IsAdmin))
		msgResp.ReplyToMessageID = msg.MessageID
		urlBtn := tgbotapi.NewInlineKeyboardButtonURL("打开官网", "https://google.com")
		msgResp.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup([]tgbotapi.InlineKeyboardButton{urlBtn})
		if _, err := b.api.Send(msgResp); err != nil {
			log.Printf("send help failed: %v", err)
		}
	case "image":
		b.handleRandomMedia(msg)
	case "images":
		if !user.IsAdmin {
			b.reply(msg, "需要管理员权限。")
			return
		}
		b.handleListMedia(msg)
	case "delimage":
		if !user.IsAdmin {
			b.reply(msg, "需要管理员权限。")
			return
		}
		b.handleDeleteMedia(msg)
	case "r2upload":
		if !user.IsAdmin {
			b.reply(msg, "需要管理员权限。")
			return
		}
		b.handleR2Upload(msg)
	case "r2list":
		if !user.IsAdmin {
			b.reply(msg, "需要管理员权限。")
			return
		}
		b.handleR2List(msg)
	case "r2del":
		if !user.IsAdmin {
			b.reply(msg, "需要管理员权限。")
			return
		}
		b.handleR2Delete(msg)
	case "checkin":
		gained, updated, err := b.store.CheckIn(user.ID, checkInReward)
		if err != nil {
			b.reply(msg, "签到失败，请稍后再试。")
			return
		}
		if gained == 0 {
			b.reply(msg, fmt.Sprintf("今天已经签到过啦！当前积分：%d", updated.Points))
			return
		}
		b.reply(msg, fmt.Sprintf("签到成功，获得 %d 积分！当前积分：%d", gained, updated.Points))
	case "points", "me":
		b.reply(msg, fmt.Sprintf("当前积分：%d", user.Points))
	case "users":
		if !user.IsAdmin {
			b.reply(msg, "只有管理员可以查看用户列表。")
			return
		}
		b.handleListUsers(msg)
	case "setpoints":
		if !user.IsAdmin {
			b.reply(msg, "需要管理员权限。")
			return
		}
		b.handleSetPoints(msg)
	case "ratelimit":
		if !user.IsAdmin {
			b.reply(msg, "需要管理员权限。")
			return
		}
		b.handleRateLimit(msg)
	case "setratelimit":
		if !user.IsAdmin {
			b.reply(msg, "需要管理员权限。")
			return
		}
		b.handleSetRateLimit(msg)
	case "addpoints":
		if !user.IsAdmin {
			b.reply(msg, "需要管理员权限。")
			return
		}
		b.handleAddPoints(msg)
	case "setmodel":
		if !user.IsAdmin {
			b.reply(msg, "需要管理员权限。")
			return
		}
		b.handleSetModel(msg)
	case "setadmin":
		if !user.IsAdmin {
			b.reply(msg, "需要管理员权限。")
			return
		}
		b.handleSetAdmin(msg)
	default:
		b.reply(msg, "未知指令，发送 /help 查看可用命令。")
	}
}

func (b *Bot) handleChat(user *store.User, msg *tgbotapi.Message) {
	if user.Points < chatCost {
		b.reply(msg, "积分不足，先签到获取积分吧！")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	answer, err := b.chat.Chat(ctx, user.ID, msg.Text)
	if err != nil {
		log.Printf("chat error: %v", err)
		b.reply(msg, fmt.Sprintf("聊天服务不可用，请稍后再试。(原因：%v)", err))
		return
	}
	if _, err := b.store.AddPoints(user.ID, -chatCost); err != nil {
		log.Printf("deduct points failed: %v", err)
	}
	b.reply(msg, answer)
}

func (b *Bot) handleRandomMedia(msg *tgbotapi.Message) {
    media, err := b.store.GetRandomMedia()
    if err != nil {
        b.reply(msg, "获取失败。")
        return
    }
    if media == nil {
        b.reply(msg, "媒体库为空。")
        return
    }
    
    var share tgbotapi.Chattable
    if media.Type == "video" {
        v := tgbotapi.NewVideo(msg.Chat.ID, tgbotapi.FileID(media.FileID))
        v.Caption = media.Caption
        share = v
    } else {
        p := tgbotapi.NewPhoto(msg.Chat.ID, tgbotapi.FileID(media.FileID))
        p.Caption = media.Caption
        share = p
    }

    if _, err := b.api.Send(share); err != nil {
        log.Printf("send random media failed: %v", err)
         b.reply(msg, "发送失败，可能文件已过期。")
    }
}

func (b *Bot) handleR2Upload(msg *tgbotapi.Message) {
    if b.r2 == nil {
        b.reply(msg, "R2 未配置。")
        return
    }
    if msg.ReplyToMessage == nil || len(msg.ReplyToMessage.Photo) == 0 {
        b.reply(msg, "请回复一张图片进行上传。")
        return
    }
    
    // Get file info
    photo := msg.ReplyToMessage.Photo[len(msg.ReplyToMessage.Photo)-1]
    fileInfo, err := b.api.GetFile(tgbotapi.FileConfig{FileID: photo.FileID})
    if err != nil {
        b.reply(msg, "获取文件信息失败。")
        return
    }
    
    // Download file
    fileURL := fileInfo.Link(b.cfg.BotToken)
    resp, err := http.Get(fileURL)
    if err != nil {
        b.reply(msg, fmt.Sprintf("下载失败：%v", err))
        return
    }
    defer resp.Body.Close()
    
    data, err := io.ReadAll(resp.Body)
    if err != nil {
         b.reply(msg, fmt.Sprintf("读取失败：%v", err))
         return
    }

    key := fmt.Sprintf("tg_%s_%d.jpg", photo.FileID, time.Now().Unix())
    url, err := b.r2.Upload(key, data, "image/jpeg")
    if err != nil {
        b.reply(msg, fmt.Sprintf("上传 R2 失败：%v", err))
        return
    }
    
    b.reply(msg, fmt.Sprintf("上传成功！\nKey: %s\nURL: %s", key, url))
}

func (b *Bot) handleR2List(msg *tgbotapi.Message) {
     if b.r2 == nil {
        b.reply(msg, "R2 未配置。")
        return
    }
    keys, err := b.r2.List()
    if err != nil {
        b.reply(msg, fmt.Sprintf("列表获取失败：%v", err))
        return
    }
    if len(keys) == 0 {
		b.reply(msg, "R2 存储桶为空。")
		return
	}
    
    var buffer bytes.Buffer
    buffer.WriteString("R2 文件列表：\n")
    for _, k := range keys {
        buffer.WriteString(fmt.Sprintf("- %s\n", k))
    }
    b.reply(msg, buffer.String())
}

func (b *Bot) handleR2Delete(msg *tgbotapi.Message) {
     if b.r2 == nil {
        b.reply(msg, "R2 未配置。")
        return
    }
    args := strings.Fields(msg.CommandArguments())
    if len(args) != 1 {
        b.reply(msg, "用法：/r2del <key>")
        return
    }
    if err := b.r2.Delete(args[0]); err != nil {
        b.reply(msg, fmt.Sprintf("删除失败：%v", err))
        return
    }
    b.reply(msg, "删除成功。")
}

func (b *Bot) handleListMedia(msg *tgbotapi.Message) {
    page := 1
    if msg.ReplyToMessage != nil && msg.ReplyToMessage.ReplyMarkup != nil {
        // Callback handling logic will go here
    }
    b.showMediaList(msg.Chat.ID, page)
}

func (b *Bot) showMediaList(chatID int64, page int) {
	limit := 5
	offset := (page - 1) * limit
	list, err := b.store.ListMedia(limit, offset)
	if err != nil {
		log.Printf("list media failed: %v", err)
		return
	}
	if len(list) == 0 && page == 1 {
		msg := tgbotapi.NewMessage(chatID, "媒体库为空。")
		b.api.Send(msg)
		return
	}
	
    resp := tgbotapi.NewMessage(chatID, fmt.Sprintf("媒体列表 (第 %d 页)：", page))
    var rows [][]tgbotapi.InlineKeyboardButton
    
    for _, m := range list {
        label := m.Caption
        if label == "" {
            label = "无标题"
        }
        if len([]rune(label)) > 10 {
            label = string([]rune(label)[:10]) + "..."
        }
        typeIcon := "📷"
        if m.Type == "video" {
            typeIcon = "📹"
        }
        
        btn := tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("%s %s [删]", typeIcon, label), fmt.Sprintf("del_media:%s", m.ID))
        rows = append(rows, []tgbotapi.InlineKeyboardButton{btn})
    }

	// Pagination buttons
	var navRow []tgbotapi.InlineKeyboardButton
	if page > 1 {
		navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData("上一页", fmt.Sprintf("list_media:%d", page-1)))
	}
	if len(list) == limit {
		navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData("下一页", fmt.Sprintf("list_media:%d", page+1)))
	}
	if len(navRow) > 0 {
		rows = append(rows, navRow)
	}

	resp.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(rows...)
	if _, err := b.api.Send(resp); err != nil {
		log.Printf("send media list failed: %v", err)
	}
}

func (b *Bot) handleDeleteMedia(msg *tgbotapi.Message) {
    args := strings.Fields(msg.CommandArguments())
    if len(args) != 1 {
        b.reply(msg, "用法：/delimage <id>")
        return
    }
    id := args[0]
    if err := b.store.DeleteMedia(id); err != nil {
        b.reply(msg, fmt.Sprintf("删除失败：%v", err))
        return
    }
    b.reply(msg, "删除成功。")
}

func (b *Bot) handleListUsers(msg *tgbotapi.Message) {
	page := 1
	if msg.ReplyToMessage != nil && msg.ReplyToMessage.ReplyMarkup != nil {
		// This might be a callback update, but simpler to just default to 1 for command
	}
	b.showUserList(msg.Chat.ID, page)
}

func (b *Bot) showUserList(chatID int64, page int) {
	limit := 10
	offset := (page - 1) * limit
	users, err := b.store.ListUsers(limit, offset)
	if err != nil {
		log.Printf("list users failed: %v", err)
		return
	}
	if len(users) == 0 && page == 1 {
		msg := tgbotapi.NewMessage(chatID, "暂无用户。")
		b.api.Send(msg)
		return
	}
	var rows [][]tgbotapi.InlineKeyboardButton
	for i, u := range users {
		label := fmt.Sprintf("%s(%d) 积分:%d", strings.TrimSpace(u.DisplayName), u.ID, u.Points)
		if label == "(0) 积分:0" {
			label = fmt.Sprintf("用户%d 积分:%d", u.ID, u.Points)
		}
		btn := tgbotapi.NewInlineKeyboardButtonData(label, fmt.Sprintf("user:%d", u.ID))
		if i%2 == 0 {
			rows = append(rows, []tgbotapi.InlineKeyboardButton{btn})
		} else {
			rows[len(rows)-1] = append(rows[len(rows)-1], btn)
		}
	}

	// Pagination buttons
	var navRow []tgbotapi.InlineKeyboardButton
	if page > 1 {
		navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData("上一页", fmt.Sprintf("list_users:%d", page-1)))
	}
	// Simple check if we might have more: if we got a full page, assume there might be more.
	// Or we could count total, but for now just show Next if we have 'limit' items.
	if len(users) == limit {
		navRow = append(navRow, tgbotapi.NewInlineKeyboardButtonData("下一页", fmt.Sprintf("list_users:%d", page+1)))
	}
	if len(navRow) > 0 {
		rows = append(rows, navRow)
	}

	keyboard := tgbotapi.NewInlineKeyboardMarkup(rows...)
	resp := tgbotapi.NewMessage(chatID, fmt.Sprintf("用户列表 (第 %d 页)：", page))
	resp.ReplyMarkup = keyboard
	if _, err := b.api.Send(resp); err != nil {
		log.Printf("send user list failed: %v", err)
	}
}



func (b *Bot) handleSetPoints(msg *tgbotapi.Message) {
	args := strings.Fields(msg.CommandArguments())
	if len(args) != 2 {
		b.reply(msg, "用法：/setpoints <user_id> <points>")
		return
	}
	userID, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		b.reply(msg, "用户ID格式错误。")
		return
	}
	points, err := strconv.Atoi(args[1])
	if err != nil {
		b.reply(msg, "积分格式错误。")
		return
	}
	updated, err := b.store.SetPoints(userID, points)
	if err != nil {
		b.reply(msg, fmt.Sprintf("修改失败：%v", err))
		return
	}
	b.reply(msg, fmt.Sprintf("用户 %d 新积分：%d", updated.ID, updated.Points))
}

func (b *Bot) handleRateLimit(msg *tgbotapi.Message) {
	limit := b.chat.RateLimit()
	if limit <= 0 {
		b.reply(msg, "当前聊天速率限制：未限制（无限制）。使用 /setratelimit <每分钟次数> 可设置，设为 0 可关闭限制。")
		return
	}
	b.reply(msg, fmt.Sprintf("当前聊天速率限制：每分钟 %d 次。使用 /setratelimit <每分钟次数> 可调整，设为 0 可关闭限制。", limit))
}

func (b *Bot) handleSetRateLimit(msg *tgbotapi.Message) {
	args := strings.Fields(msg.CommandArguments())
	if len(args) != 1 {
		b.reply(msg, "用法：/setratelimit <每分钟次数>（设为 0 关闭限制）")
		return
	}
	limit, err := strconv.Atoi(args[0])
	if err != nil {
		b.reply(msg, "参数格式错误，请输入整数。")
		return
	}
	if err := b.chat.SetRateLimit(limit); err != nil {
		b.reply(msg, fmt.Sprintf("设置失败：%v", err))
		return
	}
	if limit <= 0 {
		b.reply(msg, "已关闭聊天速率限制。")
		return
	}
	b.reply(msg, fmt.Sprintf("聊天速率限制已更新为每分钟 %d 次。", limit))
}

func (b *Bot) handleAddPoints(msg *tgbotapi.Message) {
	args := strings.Fields(msg.CommandArguments())
	if len(args) != 2 {
		b.reply(msg, "用法：/addpoints <user_id> <delta>")
		return
	}
	userID, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		b.reply(msg, "用户ID格式错误。")
		return
	}
	delta, err := strconv.Atoi(args[1])
	if err != nil {
		b.reply(msg, "增减值格式错误。")
		return
	}
	updated, err := b.store.AddPoints(userID, delta)
	if err != nil {
		b.reply(msg, fmt.Sprintf("修改失败：%v", err))
		return
	}
	b.reply(msg, fmt.Sprintf("用户 %d 当前积分：%d", updated.ID, updated.Points))
}

func (b *Bot) handleSetModel(msg *tgbotapi.Message) {
	model := strings.TrimSpace(msg.CommandArguments())
	if model == "" {
		b.showModelList(msg.Chat.ID)
		return
	}
	if err := b.chat.SetModel(model); err != nil {
		b.reply(msg, fmt.Sprintf("设置失败：%v", err))
		return
	}
	b.reply(msg, fmt.Sprintf("已更新模型为 %s", model))
}

func (b *Bot) handleSetAdmin(msg *tgbotapi.Message) {
	args := strings.Fields(msg.CommandArguments())
	if len(args) != 1 {
		b.reply(msg, "用法：/setadmin <user_id>")
		return
	}
	userID, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		b.reply(msg, "用户ID格式错误。")
		return
	}
	updated, err := b.store.PromoteAdmin(userID)
	if err != nil {
		b.reply(msg, fmt.Sprintf("设置管理员失败：%v", err))
		return
	}
	b.reply(msg, fmt.Sprintf("用户 %d 已成为管理员。", updated.ID))
}

func (b *Bot) reply(msg *tgbotapi.Message, text string) {
	resp := tgbotapi.NewMessage(msg.Chat.ID, text)
	resp.ReplyToMessageID = msg.MessageID
	if _, err := b.api.Send(resp); err != nil {
		log.Printf("send message failed: %v", err)
	}
}

func (b *Bot) helpText(isAdmin bool) string {
	common := "欢迎使用积分机器人！\n" +
		"/checkin - 签到获取积分（每日一次，东八区）\n" +
		"/points - 查看当前积分\n" +
		"直接发送消息即可与机器人聊天，聊天会消耗积分。如果聊天服务不可用，机器人会提示错误原因。"
	if !isAdmin {
		return common
	}
	return common + "\n管理员命令：\n" +
		"/users - 查看用户列表和积分\n" +
		"/addpoints <user_id> <delta> - 调整积分\n" +
		"/setpoints <user_id> <points> - 设定积分\n" +
		"/ratelimit - 查看当前聊天速率限制\n" +
        "/ratelimit - 查看当前聊天速率限制\n" +
		"/setratelimit <每分钟次数> - 设置聊天速率限制（0 表示不限）\n" +
		"/setmodel <model> - 设置聊天模型\n" +
		"/setadmin <user_id> - 赋予管理员权限\n" +
		"/images - 管理图片列表\n" +
		"/delimage <id> - 删除图片"
}

func (b *Bot) handleCallback(cb *tgbotapi.CallbackQuery) {
	if _, err := b.api.Request(tgbotapi.NewCallback(cb.ID, "")); err != nil {
		log.Printf("callback ack failed: %v", err)
	}
	data := cb.Data
	switch {
	case strings.HasPrefix(data, "user:"):
		b.handleUserSelection(cb)
	case strings.HasPrefix(data, "add:"):
		b.handleAdjustPoints(cb)
	case strings.HasPrefix(data, "add_custom:"):
		b.handleCustomPointsRequest(cb)
	case strings.HasPrefix(data, "promote:"):
		b.handlePromote(cb)
	case strings.HasPrefix(data, "setmodel:"):
		b.handleModelSelection(cb)
    case strings.HasPrefix(data, "list_media:"):
        parts := strings.Split(data, ":")
        if len(parts) == 2 {
            page, _ := strconv.Atoi(parts[1])
            b.showMediaList(cb.Message.Chat.ID, page)
        }
    case strings.HasPrefix(data, "del_media:"):
        parts := strings.Split(data, ":")
        if len(parts) == 2 {
            // Check admin
            if _, ok := b.ensureAdmin(cb); !ok {
                return
            }
            id := parts[1]
            b.store.DeleteMedia(id)
            // Refresh list (stay on page 1 for simplicity)
            b.showMediaList(cb.Message.Chat.ID, 1)
        }
	case strings.HasPrefix(data, "list_users:"):
		parts := strings.Split(data, ":")
		if len(parts) == 2 {
			page, _ := strconv.Atoi(parts[1])
			b.showUserList(cb.Message.Chat.ID, page)
		}
	default:
		log.Printf("unknown callback: %s", data)
	}
}

func (b *Bot) ensureAdmin(cb *tgbotapi.CallbackQuery) (*store.User, bool) {
	user, err := b.store.GetOrCreateUser(cb.From.ID, cb.From.UserName, strings.TrimSpace(fmt.Sprintf("%s %s", cb.From.FirstName, cb.From.LastName)))
	if err != nil {
		log.Printf("load user in callback failed: %v", err)
		return nil, false
	}
	if !user.IsAdmin {
		msg := tgbotapi.NewMessage(cb.Message.Chat.ID, "需要管理员权限才能执行此操作。")
		if _, err := b.api.Send(msg); err != nil {
			log.Printf("send admin warning failed: %v", err)
		}
		return nil, false
	}
	return user, true
}

func (b *Bot) handleUserSelection(cb *tgbotapi.CallbackQuery) {
	if _, ok := b.ensureAdmin(cb); !ok {
		return
	}
	parts := strings.Split(cb.Data, ":")
	if len(parts) != 2 {
		return
	}
	targetID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return
	}
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		[]tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData("+10", fmt.Sprintf("add:%d:10", targetID)),
			tgbotapi.NewInlineKeyboardButtonData("+100", fmt.Sprintf("add:%d:100", targetID)),
			tgbotapi.NewInlineKeyboardButtonData("+500", fmt.Sprintf("add:%d:500", targetID)),
		},
		[]tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData("自定义", fmt.Sprintf("add_custom:%d", targetID)),
		},
		[]tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonData("设为管理员", fmt.Sprintf("promote:%d", targetID)),
		},
	)
	msg := tgbotapi.NewMessage(cb.Message.Chat.ID, fmt.Sprintf("请选择要对用户 %d 进行的操作：", targetID))
	msg.ReplyMarkup = keyboard
	if _, err := b.api.Send(msg); err != nil {
		log.Printf("send user actions failed: %v", err)
	}
}

func (b *Bot) handleCustomPointsRequest(cb *tgbotapi.CallbackQuery) {
	if _, ok := b.ensureAdmin(cb); !ok {
		return
	}
	parts := strings.Split(cb.Data, ":")
	if len(parts) != 2 {
		return
	}
	targetID := parts[1]
	if _, err := strconv.ParseInt(targetID, 10, 64); err != nil {
		return
	}

	b.userStates.Store(cb.From.ID, fmt.Sprintf("waiting_custom_points:%s", targetID))
	msg := tgbotapi.NewMessage(cb.Message.Chat.ID, fmt.Sprintf("请输入要给用户 %s 增加的积分（支持负数）：", targetID))
	if _, err := b.api.Send(msg); err != nil {
		log.Printf("send input prompt failed: %v", err)
	}
}

func (b *Bot) handleAdjustPoints(cb *tgbotapi.CallbackQuery) {
	if _, ok := b.ensureAdmin(cb); !ok {
		return
	}
	parts := strings.Split(cb.Data, ":")
	if len(parts) != 3 {
		return
	}
	targetID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return
	}
	delta, err := strconv.Atoi(parts[2])
	if err != nil {
		return
	}
	updated, err := b.store.AddPoints(targetID, delta)
	if err != nil {
		msg := tgbotapi.NewMessage(cb.Message.Chat.ID, fmt.Sprintf("修改失败：%v", err))
		if _, e := b.api.Send(msg); e != nil {
			log.Printf("send error msg failed: %v", e)
		}
		return
	}
	msg := tgbotapi.NewMessage(cb.Message.Chat.ID, fmt.Sprintf("用户 %d 当前积分：%d", updated.ID, updated.Points))
	if _, err := b.api.Send(msg); err != nil {
		log.Printf("send adjust result failed: %v", err)
	}
}

func (b *Bot) handlePromote(cb *tgbotapi.CallbackQuery) {
	if _, ok := b.ensureAdmin(cb); !ok {
		return
	}
	parts := strings.Split(cb.Data, ":")
	if len(parts) != 2 {
		return
	}
	targetID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return
	}
	updated, err := b.store.PromoteAdmin(targetID)
	if err != nil {
		msg := tgbotapi.NewMessage(cb.Message.Chat.ID, fmt.Sprintf("设置管理员失败：%v", err))
		if _, e := b.api.Send(msg); e != nil {
			log.Printf("send promote error failed: %v", e)
		}
		return
	}
	msg := tgbotapi.NewMessage(cb.Message.Chat.ID, fmt.Sprintf("用户 %d 已成为管理员。", updated.ID))
	if _, err := b.api.Send(msg); err != nil {
		log.Printf("send promote result failed: %v", err)
	}
}

func (b *Bot) showModelList(chatID int64) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	models, err := b.chat.ListModels(ctx)
	if err != nil {
		msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("获取模型列表失败：%v", err))
		if _, e := b.api.Send(msg); e != nil {
			log.Printf("send model list error failed: %v", e)
		}
		return
	}
	if len(models) == 0 {
		msg := tgbotapi.NewMessage(chatID, "没有可用的模型。")
		if _, e := b.api.Send(msg); e != nil {
			log.Printf("send empty model message failed: %v", e)
		}
		return
	}

	currentModel := b.chat.Model()
	var rows [][]tgbotapi.InlineKeyboardButton
	for i, m := range models {
		label := m
		if m == currentModel {
			label += " ✅"
		}
		btn := tgbotapi.NewInlineKeyboardButtonData(label, fmt.Sprintf("setmodel:%s", m))
		if i%2 == 0 {
			rows = append(rows, []tgbotapi.InlineKeyboardButton{btn})
		} else {
			rows[len(rows)-1] = append(rows[len(rows)-1], btn)
		}
	}
	keyboard := tgbotapi.NewInlineKeyboardMarkup(rows...)
	msg := tgbotapi.NewMessage(chatID, "请选择要使用的模型：")
	msg.ReplyMarkup = keyboard
	if _, err := b.api.Send(msg); err != nil {
		log.Printf("send model list failed: %v", err)
	}
}

func (b *Bot) handleModelSelection(cb *tgbotapi.CallbackQuery) {
	if _, ok := b.ensureAdmin(cb); !ok {
		return
	}
	parts := strings.SplitN(cb.Data, ":", 2)
	if len(parts) != 2 {
		return
	}
	model := parts[1]
	if err := b.chat.SetModel(model); err != nil {
		msg := tgbotapi.NewMessage(cb.Message.Chat.ID, fmt.Sprintf("设置失败：%v", err))
		if _, e := b.api.Send(msg); e != nil {
			log.Printf("send model error failed: %v", e)
		}
		return
	}
	msg := tgbotapi.NewMessage(cb.Message.Chat.ID, fmt.Sprintf("已更新模型为 %s", model))
	if _, err := b.api.Send(msg); err != nil {
		log.Printf("send model updated failed: %v", err)
	}
}
