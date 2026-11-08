package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const telegramAPI = "https://api.telegram.org/bot"

type Update struct {
	UpdateID int64 `json:"update_id"`
	Message  *struct {
		Chat struct {
			ID int64 `json:"id"`
		} `json:"chat"`
		From struct {
			ID int64 `json:"id"`
		} `json:"from"`
		Text string `json:"text"`
	} `json:"message"`
}

type telegramResponse struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	Description string          `json:"description"`
}

func (b *Bot) telegram(ctx context.Context, method string, params url.Values, out any) error {
	var body io.Reader
	if params != nil {
		body = strings.NewReader(params.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, telegramAPI+b.cfg.Telegram.BotToken+"/"+method, body)
	if err != nil {
		return errors.New("invalid Telegram request")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := b.client.Do(req)
	if err != nil {
		// The bot token is part of the request URL; do not return transport errors to logs.
		return errors.New("Telegram request failed")
	}
	defer resp.Body.Close()
	var response telegramResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&response); err != nil {
		return errors.New("invalid Telegram API response")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || !response.OK {
		return fmt.Errorf("Telegram API returned %s: %s", resp.Status, response.Description)
	}
	if out != nil {
		return json.Unmarshal(response.Result, out)
	}
	return nil
}

func (b *Bot) pollUpdates(ctx context.Context) error {
	var updates []Update
	err := b.telegram(ctx, "getUpdates", url.Values{"offset": {strconv.FormatInt(b.offset, 10)}, "timeout": {"20"}, "allowed_updates": {`["message"]`}}, &updates)
	if err != nil {
		return err
	}
	for _, update := range updates {
		b.offset = update.UpdateID + 1
		if update.Message != nil {
			b.handleMessage(ctx, update.Message.Chat.ID, update.Message.From.ID, update.Message.Text)
		}
	}
	return nil
}

func (b *Bot) send(ctx context.Context, chatID int64, text string) error {
	return b.telegram(ctx, "sendMessage", url.Values{"chat_id": {strconv.FormatInt(chatID, 10)}, "text": {text}}, nil)
}

func (b *Bot) notifyAll(ctx context.Context, text string) error {
	rows, err := b.db.QueryContext(ctx, "SELECT chat_id FROM users")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var chatID int64
		if err := rows.Scan(&chatID); err != nil {
			return err
		}
		if err := b.send(ctx, chatID, text); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (b *Bot) notifyAllHTML(ctx context.Context, text string) error {
	rows, err := b.db.QueryContext(ctx, "SELECT chat_id FROM users")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var chatID int64
		if err := rows.Scan(&chatID); err != nil {
			return err
		}
		params := url.Values{
			"chat_id":    {strconv.FormatInt(chatID, 10)},
			"text":       {text},
			"parse_mode": {"HTML"},
		}
		if err := b.telegram(ctx, "sendMessage", params, nil); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (b *Bot) Run(ctx context.Context) error {
	log.Printf("bot started; spread polling interval=%s", b.cfg.PollInterval)
	go func() {
		for ctx.Err() == nil {
			if err := b.pollUpdates(ctx); err != nil && !errors.Is(err, context.Canceled) {
				// Do not include request errors, which could contain a credential-bearing URL.
				select {
				case <-ctx.Done():
					return
				case <-time.After(2 * time.Second):
				}
			}
		}
	}()
	ticker := time.NewTicker(b.cfg.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := b.evaluateAll(ctx); err != nil {
				// Avoid logging provider response bodies that might contain sensitive data.
				log.Printf("spread evaluation failed")
			}
		}
	}
}
