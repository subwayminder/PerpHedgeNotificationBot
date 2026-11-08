package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	_ "modernc.org/sqlite"
)

type Bot struct {
	cfg           Config
	db            *sql.DB
	client        *http.Client
	offset        int64
	medianCacheMu sync.Mutex
	medianCache   map[string]cachedMedian
}

func NewBot(ctx context.Context) (*Bot, error) {
	// Keep the database and SQLite WAL/SHM files private even when the database
	// is stored beside the project files instead of in a private subdirectory.
	syscall.Umask(0077)
	cfg, err := LoadConfig()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(cfg.Database.Path), 0700); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}
	db, err := sql.Open("sqlite", cfg.Database.Path)
	if err != nil {
		return nil, fmt.Errorf("open SQLite database: %w", err)
	}
	db.SetMaxOpenConns(1)
	if err := initDB(ctx, db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("initialize SQLite database: %w", err)
	}
	for _, path := range []string{cfg.Database.Path, cfg.Database.Path + "-wal", cfg.Database.Path + "-shm"} {
		if err := os.Chmod(path, 0600); err != nil && !os.IsNotExist(err) {
			_ = db.Close()
			return nil, fmt.Errorf("secure SQLite file: %w", err)
		}
	}
	return &Bot{cfg: cfg, db: db, client: &http.Client{Timeout: 30 * time.Second}}, nil
}

func (b *Bot) Close() error { return b.db.Close() }
