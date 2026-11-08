package main

import (
	"context"
	"errors"
	"log"
	"os/signal"
	"syscall"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	bot, err := NewBot(ctx)
	if err != nil {
		log.Fatal(err)
	}
	defer bot.Close()

	if err := bot.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatal(err)
	}
}
