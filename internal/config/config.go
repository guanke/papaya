package config

import (
	"fmt"
	"os"
	"strings"
)

// Config holds runtime configuration derived from environment variables.
type Config struct {
	BotToken     string
	AdminIDs     []int64
	OpenAIKey    string
	OpenAIBase   string
	Model        string
	DataFilePath string

    // Cloudflare R2
    R2AccountID       string
    R2AccessKeyID     string
    R2SecretAccessKey string
    R2BucketName      string
    R2PublicURL       string // Optional
}

// Load parses environment variables into Config.
func Load() (*Config, error) {
	cfg := &Config{
		BotToken:     os.Getenv("TG_BOT_SECRET"),
		OpenAIKey:    os.Getenv("OPENAI_API_KEY"),
		OpenAIBase:   os.Getenv("OPENAI_BASE_URL"),
		Model:        os.Getenv("OPENAI_MODEL"),
		DataFilePath: envOrDefault("DATA_FILE", "data.db"),

        R2AccountID:       os.Getenv("R2_ACCOUNT_ID"),
        R2AccessKeyID:     os.Getenv("R2_ACCESS_KEY_ID"),
        R2SecretAccessKey: os.Getenv("R2_SECRET_ACCESS_KEY"),
        R2BucketName:      os.Getenv("R2_BUCKET_NAME"),
        R2PublicURL:       os.Getenv("R2_PUBLIC_URL"),
	}

	if cfg.BotToken == "" {
		return nil, fmt.Errorf("TG_BOT_SECRET is required")
	}

	if cfg.Model == "" {
		cfg.Model = "gpt-3.5-turbo"
	}

	admins := strings.FieldsFunc(os.Getenv("TG_ADMIN_IDS"), func(r rune) bool { return r == ',' || r == ' ' })
	for _, id := range admins {
		if id == "" {
			continue
		}
		value, err := parseInt64(id)
		if err != nil {
			return nil, fmt.Errorf("invalid TG_ADMIN_IDS entry %q: %w", id, err)
		}
		cfg.AdminIDs = append(cfg.AdminIDs, value)
	}

	return cfg, nil
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func parseInt64(value string) (int64, error) {
	var result int64
	_, err := fmt.Sscan(value, &result)
	return result, err
}
