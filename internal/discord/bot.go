package discord

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/guanke/papaya/internal/chat"
	"github.com/guanke/papaya/internal/config"
	"github.com/guanke/papaya/internal/store"
)

const (
	checkInReward = 10
	chatCost      = 1
)

type Bot struct {
	session *discordgo.Session
	store   *store.Store
	chat    *chat.Manager
	cfg     *config.Config
}

func New(cfg *config.Config, st *store.Store, manager *chat.Manager) (*Bot, error) {
	// Discord Token should be added to config. For now assuming cfg has it or we pass it separately?
	// Config refactoring might be needed, but I'll assume we add DiscordToken to Config struct later.
	// For now, I'll update Config struct in main.go plan.
	
	// Temporarily relying on a new field in config or env var. 
	// Let's assume Config has DiscordToken.
	
	dg, err := discordgo.New("Bot " + cfg.DiscordToken)
	if err != nil {
		return nil, err
	}

	return &Bot{
		session: dg,
		store:   st,
		chat:    manager,
		cfg:     cfg,
	}, nil
}

func (b *Bot) Run(ctx context.Context) error {
	b.session.AddHandler(b.handleMessage)
	
	// Register slash commands (optional, but good for Discord)
	// For simplicity, we can start with message parsing like Telegram.
	// But Discord encourages Slash Commands.
	// Let's stick to prefix commands first to mirror Telegram logic quickly.
	
	b.session.Identify.Intents = discordgo.IntentsGuildMessages | discordgo.IntentsDirectMessages | discordgo.IntentsMessageContent

	if err := b.session.Open(); err != nil {
		return err
	}
	slog.Info("Discord Bot started")

	<-ctx.Done()
	return b.session.Close()
}

func (b *Bot) handleMessage(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author.ID == s.State.User.ID {
		return
	}

	userID := m.Author.ID // String already
	username := m.Author.Username
	displayName := m.Author.GlobalName
	if displayName == "" {
		displayName = username
	}

	user, err := b.store.GetOrCreateUser(userID, username, displayName)
	if err != nil {
		slog.Error("failed to get user", "error", err)
		return
	}

	for _, id := range b.cfg.AdminIDs {
		if id == user.ID && !user.IsAdmin {
			if _, err := b.store.PromoteAdmin(user.ID); err != nil {
				slog.Error("promote admin failed", "error", err)
			}
			user.IsAdmin = true
		}
	}

	// Commands
	if strings.HasPrefix(m.Content, "/") {
		args := strings.Fields(m.Content)
		cmd := args[0]
		
		switch cmd {
		case "/checkin":
			gained, updated, err := b.store.CheckIn(user.ID, checkInReward)
			if err != nil {
				s.ChannelMessageSend(m.ChannelID, "Check-in failed.")
				return
			}
			if gained == 0 {
				s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Already checked in today! Points: %d", updated.Points))
			} else {
				s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Check-in success! +%d. Points: %d", gained, updated.Points))
			}
		case "/points":
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Points: %d", user.Points))
		case "/help":
			s.ChannelMessageSend(m.ChannelID, "Commands: /checkin, /points, /help. Chat with me directly or mention me.")
		default:
			// Unknown command, ignore or help
		}
		return
	}

	// Chat Logic
	// If DM or Mentioned
	isDM := m.GuildID == ""
	isMentioned := false
	for _, u := range m.Mentions {
		if u.ID == s.State.User.ID {
			isMentioned = true
			break
		}
	}

	if isDM || isMentioned {
		prompt := m.Content
		// Remove mention
		prompt = strings.ReplaceAll(prompt, "<@"+s.State.User.ID+">", "")
		prompt = strings.TrimSpace(prompt)

		if prompt == "" {
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		answer, err := b.chat.Chat(ctx, user, prompt)
		if err != nil {
			slog.Error("chat error", "error", err)
			s.ChannelMessageSend(m.ChannelID, "Chat unavailable.")
			return
		}
		
		// Deduct points?
		if _, err := b.store.AddPoints(user.ID, -chatCost); err != nil {
			slog.Error("deduct points failed", "err", err)
		}
		
		s.ChannelMessageSend(m.ChannelID, answer)
	}
}
