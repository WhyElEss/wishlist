package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

type Config struct {
	Listen    string
	DataDir   string
	AdminHash string
	// BaseURL is the public origin (https://wishlist.example.com). It is used
	// for links in e-mails and as an accepted Origin for form posts.
	BaseURL string
	// TrustProxy makes the app read the client IP and country from the
	// Cloudflare headers (CF-Connecting-IP, CF-IPCountry).
	TrustProxy bool

	TelegramToken string
	TelegramChat  string

	SMTPHost string
	SMTPPort string
	SMTPUser string
	SMTPPass string
	SMTPFrom string
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return def
}

func loadConfig() (Config, error) {
	c := Config{
		Listen:        env("LISTEN_ADDR", ":8080"),
		DataDir:       env("DATA_DIR", "./data"),
		AdminHash:     env("ADMIN_PASSWORD_HASH", ""),
		BaseURL:       strings.TrimRight(env("BASE_URL", ""), "/"),
		TrustProxy:    env("TRUST_PROXY_HEADERS", "true") != "false",
		TelegramToken: env("TELEGRAM_BOT_TOKEN", ""),
		TelegramChat:  env("TELEGRAM_CHAT_ID", ""),
		SMTPHost:      env("SMTP_HOST", ""),
		SMTPPort:      env("SMTP_PORT", "587"),
		SMTPUser:      env("SMTP_USER", ""),
		SMTPPass:      env("SMTP_PASSWORD", ""),
		SMTPFrom:      env("SMTP_FROM", ""),
	}
	if c.AdminHash == "" {
		return c, errors.New("ADMIN_PASSWORD_HASH is not set; generate it with: wishlist hashpw")
	}
	if _, err := parseHash(c.AdminHash); err != nil {
		return c, fmt.Errorf("ADMIN_PASSWORD_HASH: %w", err)
	}
	if c.SMTPHost != "" && c.BaseURL == "" {
		return c, errors.New("SMTP_HOST is set but BASE_URL is empty: e-mails need BASE_URL for their links")
	}
	return c, nil
}
