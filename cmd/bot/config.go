package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Telegram struct {
		BotToken       string  `yaml:"bot_token"`
		AllowedUserIDs []int64 `yaml:"allowed_user_ids"`
	} `yaml:"telegram"`
	Database struct {
		Path string `yaml:"path"`
	} `yaml:"database"`
	Polling struct {
		IntervalSeconds int `yaml:"interval_seconds"`
	} `yaml:"polling"`
	Lighter struct {
		CoreAPI      string `yaml:"core_api"`
		RobinhoodAPI string `yaml:"robinhood_api"`
	} `yaml:"lighter"`

	PollInterval time.Duration
	AllowedUsers map[int64]bool
}

func LoadConfig() (Config, error) {
	var cfg Config
	path, err := findConfig()
	if err != nil {
		return cfg, err
	}
	file, err := os.Open(path)
	if err != nil {
		return cfg, fmt.Errorf("open config.yaml: %w", err)
	}
	defer file.Close()
	if err := yaml.NewDecoder(file).Decode(&cfg); err != nil {
		return cfg, errors.New("could not parse config.yaml")
	}

	cfg.Telegram.BotToken = strings.TrimSpace(os.ExpandEnv(cfg.Telegram.BotToken))
	if cfg.Telegram.BotToken == "" || cfg.Telegram.BotToken == "REPLACE_WITH_BOTFATHER_TOKEN" {
		return cfg, errors.New("set TELEGRAM_BOT_TOKEN in the environment")
	}
	if cfg.Database.Path == "" {
		cfg.Database.Path = "./spreads.db"
	}
	if !filepath.IsAbs(cfg.Database.Path) {
		configPath, err := filepath.Abs(path)
		if err != nil {
			return cfg, fmt.Errorf("resolve config path: %w", err)
		}
		cfg.Database.Path = filepath.Join(filepath.Dir(configPath), cfg.Database.Path)
	}
	if cfg.Polling.IntervalSeconds == 0 {
		cfg.Polling.IntervalSeconds = 10
	}
	if cfg.Polling.IntervalSeconds < 5 || cfg.Polling.IntervalSeconds > 300 {
		return cfg, errors.New("polling.interval_seconds must be between 5 and 300")
	}
	if cfg.Lighter.CoreAPI == "" {
		cfg.Lighter.CoreAPI = "https://mainnet.zklighter.elliot.ai"
	}
	if cfg.Lighter.RobinhoodAPI == "" {
		cfg.Lighter.RobinhoodAPI = "https://api.rh.lighter.xyz"
	}
	cfg.Lighter.CoreAPI = strings.TrimRight(cfg.Lighter.CoreAPI, "/")
	cfg.Lighter.RobinhoodAPI = strings.TrimRight(cfg.Lighter.RobinhoodAPI, "/")
	cfg.PollInterval = time.Duration(cfg.Polling.IntervalSeconds) * time.Second
	cfg.AllowedUsers = make(map[int64]bool, len(cfg.Telegram.AllowedUserIDs))
	for _, id := range cfg.Telegram.AllowedUserIDs {
		if id <= 0 {
			return cfg, errors.New("telegram.allowed_user_ids must contain positive numeric IDs")
		}
		cfg.AllowedUsers[id] = true
	}
	return cfg, nil
}

func findConfig() (string, error) {
	for _, path := range []string{"config.yaml", filepath.Join("..", "..", "config.yaml")} {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
	}
	return "", errors.New("config.yaml not found; run from the project root or cmd/bot")
}
