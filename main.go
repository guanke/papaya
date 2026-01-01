package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/sync/errgroup"

	"github.com/guanke/papaya/internal/chat"
	"github.com/guanke/papaya/internal/config"
	"github.com/guanke/papaya/internal/discord"
	"github.com/guanke/papaya/internal/logger"
	"github.com/guanke/papaya/internal/store"
	"github.com/guanke/papaya/internal/telegram"
)

func main() {
	// Initialize logger (defaulting to info/text for now, could act on flags/env)
	logger.Init("info", "text")
	
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	st, err := store.New(cfg.DataFilePath)
	if err != nil {
		log.Fatalf("init store: %v", err)
	}
	defer st.Close()

	manager := chat.NewManager(cfg.OpenAIKey, cfg.OpenAIBase, cfg.Model, st)
	
	// Init Telegram Bot
	tgBot, err := telegram.New(cfg, st, manager)
	if err != nil {
		log.Fatalf("init telegram bot: %v", err)
	}

	// Init Discord Bot
	// Failure to init Discord shouldn't block Telegram if token is missing, generally.
	// But for now let's only fail if token is present but init fails.
	var dcBot *discord.Bot
	if cfg.DiscordToken != "" {
		dcBot, err = discord.New(cfg, st, manager)
		if err != nil {
			log.Printf("init discord bot failed: %v", err)
		}
	} else {
		log.Println("Discord token not provided, skipping Discord bot.")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	g, ctx := errgroup.WithContext(ctx)

	// Run Telegram
	g.Go(func() error {
		return tgBot.Run(ctx)
	})

	// Run Discord
	if dcBot != nil {
		g.Go(func() error {
			return dcBot.Run(ctx)
		})
	}

	if err := g.Wait(); err != nil {
		log.Printf("server stopped: %v", err)
		os.Exit(1)
	}
}
