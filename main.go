package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/guanke/papaya/internal/bot"
	"github.com/guanke/papaya/internal/chat"
	"github.com/guanke/papaya/internal/config"
	"github.com/guanke/papaya/internal/store"
)

func main() {
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
	b, err := bot.New(cfg, st, manager)
	if err != nil {
		log.Fatalf("init bot: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := b.Run(ctx); err != nil {
		log.Printf("bot stopped: %v", err)
		os.Exit(1)
	}
}
