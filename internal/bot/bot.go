package bot

import (
	"context"
	"fmt"
	"log"
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
	chat  *chat.Manager
	cfg   *config.Config
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

	if msg.IsCommand() {
		b.handleCommand(user, msg)
		return
	}
	b.handleChat(user, msg)
}

func (b *Bot) handleCommand(user *store.User, msg *tgbotapi.Message) {
	switch msg.Command() {
	case "start", "help":
		b.reply(msg, b.helpText(user.IsAdmin))
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
		b.reply(msg, fmt.Sprintf("聊天服务不可用：%v", err))
		return
	}
	if _, err := b.store.AddPoints(user.ID, -chatCost); err != nil {
		log.Printf("deduct points failed: %v", err)
	}
	b.reply(msg, answer)
}

func (b *Bot) handleListUsers(msg *tgbotapi.Message) {
	users, err := b.store.ListUsers()
	if err != nil {
		b.reply(msg, "无法获取用户列表。")
		return
	}
	var builder strings.Builder
	builder.WriteString("用户列表：\n")
	for _, u := range users {
		builder.WriteString(fmt.Sprintf("ID:%d 积分:%d 上次签到:%s 管理员:%t\n", u.ID, u.Points, u.LastCheckin, u.IsAdmin))
	}
	b.reply(msg, builder.String())
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
		b.reply(msg, "用法：/setmodel <model>")
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
		"直接发送消息即可与机器人聊天，聊天会消耗积分。"
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
