package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	Port          string
	DBPath        string
	SessionSecret string
	CookieSecure  bool
	SessionTTL    time.Duration
}

func Load() (Config, error) {
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return Config{}, fmt.Errorf("read .env: %w", err)
	}
	c := Config{Port: os.Getenv("PORT"), DBPath: os.Getenv("DB_PATH"), SessionSecret: os.Getenv("SESSION_SECRET"), SessionTTL: 7 * 24 * time.Hour}
	port, err := strconv.Atoi(c.Port)
	if err != nil || port < 1 || port > 65535 {
		return c, errors.New("PORT must be between 1 and 65535; copy .env.example to .env")
	}
	if strings.TrimSpace(c.DBPath) == "" {
		return c, errors.New("DB_PATH is required")
	}
	if len(c.SessionSecret) < 32 || strings.HasPrefix(c.SessionSecret, "replace-") {
		return c, errors.New("SESSION_SECRET must be a random secret of at least 32 bytes")
	}
	if v := os.Getenv("COOKIE_SECURE"); v != "" {
		c.CookieSecure, err = strconv.ParseBool(v)
		if err != nil {
			return c, errors.New("COOKIE_SECURE must be true or false")
		}
	}
	if v := os.Getenv("SESSION_TTL"); v != "" {
		c.SessionTTL, err = time.ParseDuration(v)
		if err != nil || c.SessionTTL < time.Minute || c.SessionTTL > 30*24*time.Hour {
			return c, errors.New("SESSION_TTL must be between 1m and 720h")
		}
	}
	return c, nil
}
