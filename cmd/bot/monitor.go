package main

import (
	"context"
	"fmt"
	"log"
	"time"
)

func (b *Bot) evaluateAll(ctx context.Context) error {
	markets, err := b.markets(ctx)
	if err != nil {
		return err
	}
	for _, market := range markets {
		core, err := b.fetchMarket(ctx, b.cfg.Lighter.CoreAPI, market.CoreMarket)
		if err != nil {
			log.Printf("%s Core price unavailable", market.Symbol)
			continue
		}
		rh, err := b.fetchMarket(ctx, b.cfg.Lighter.RobinhoodAPI, market.RHMarket)
		if err != nil {
			log.Printf("%s Robinhood price unavailable", market.Symbol)
			continue
		}
		spread := (rh - core) / core * 100
		now := time.Now().Unix()
		if err := b.saveSample(ctx, market.Symbol, now, core, rh, spread); err != nil {
			log.Printf("sample write failed for %s", market.Symbol)
			continue
		}
		median, count, err := b.medianSpread(ctx, market.Symbol, market.Window, now)
		if err != nil || count < 2 {
			continue
		}
		// The configured bounds apply to the current spread minus the rolling median.
		deviation := spread - median
		if deviation >= market.Low && deviation <= market.High {
			lastSentAt, err := b.lastInRangeNotification(ctx, market.Symbol)
			if err == nil && now-lastSentAt >= 5 {
				message := formatSpreadNotification(spreadMarker(spread), "Спред в диапазоне относительно медианы", market, median, spread, deviation, core, rh)
				if err := b.notifyAll(ctx, message); err != nil {
					log.Printf("notification failed for %s", market.Symbol)
				} else {
					_ = b.setLastInRangeNotification(ctx, market.Symbol, now)
				}
			}
		}
		_ = b.pruneSamples(ctx, market.Symbol, now-int64(30*24*time.Hour/time.Second))
	}
	return nil
}

func spreadMarker(spread float64) string {
	switch {
	case spread < 0:
		return "🔴"
	case spread > 0:
		return "🟢"
	default:
		return "⚪"
	}
}

func formatSpreadNotification(marker, title string, market MarketConfig, median, current, deviation, core, robinhood float64) string {
	return fmt.Sprintf("%s %s %s: медиана за %d мин. = %+.4f%%; текущий спред = %+.4f%%; отклонение от медианы = %+.4f п.п. (Core %.8g, Robinhood %.8g).", marker, title, market.Symbol, int(market.Window.Minutes()), median, current, deviation, core, robinhood)
}
