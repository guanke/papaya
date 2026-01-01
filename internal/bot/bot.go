package bot

import (
	"context"
	"fmt"
	"log"
    "sync"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/guanke/papaya/internal/chat"
	"github.com/guanke/papaya/internal/config"
	"github.com/guanke/papaya/internal/store"
)

const (
	checkInReward = 10
	chatCost      = 1
)

// Bot wires together Telegram updates, persistence, and chat backend.
type Bot struct {
	api   *tgbotapi.BotAPI
	store *store.Store
	chat       *chat.Manager
	cfg        *config.Config
	userStates sync.Map // map[int64]string (userID -> state)
}

// New creates a Bot instance.
func New(cfg *config.Config, st *store.Store, manager *chat.Manager) (*Bot, error) {
	api, err := tgbotapi.NewBotAPI(cfg.BotToken)
	if err != nil {
		return nil, err
	}
	return &Bot{api: api, store: st, chat: manager, cfg: cfg}, nil
}

// Run starts processing Telegram updates.
func (b *Bot) Run(ctx context.Context) error {
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
		"/setmodel <model> - 设置聊天模型\n" +
		"/setadmin <user_id> - 赋予管理员权限"
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

	var rows [][]tgbotapi.InlineKeyboardButton
	for i, m := range models {
		btn := tgbotapi.NewInlineKeyboardButtonData(m, fmt.Sprintf("setmodel:%s", m))
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
