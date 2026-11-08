package main

import (
	"context"
	"fmt"
	"html"
	"log"
	"sort"
	"time"
)

type cachedMedian struct {
	end                      int64
	coreMarket, rhMarket     string
	coreMarketID, rhMarketID int
	window                   time.Duration
	median                   float64
	count                    int
}

func (b *Bot) evaluateAll(ctx context.Context) error {
	markets, err := b.markets(ctx)
	if err != nil {
		return err
	}
	for _, market := range markets {
		coreID, core, err := b.fetchMarketData(ctx, b.cfg.Lighter.CoreAPI, market.CoreMarket)
		if err != nil {
			log.Printf("%s Core price unavailable", market.Symbol)
			continue
		}
		rhID, rh, err := b.fetchMarketData(ctx, b.cfg.Lighter.RobinhoodAPI, market.RHMarket)
		if err != nil {
			log.Printf("%s Robinhood price unavailable", market.Symbol)
			continue
		}
		spread := (rh - core) / core * 100
		now := time.Now().Unix()
		// Reuse the historical median throughout the current minute. Only completed
		// candles are used, so the current partial candle is excluded.
		end := now - now%60
		median, count, err := b.marketMedian(ctx, market, coreID, rhID, end)
		if err != nil {
			log.Printf("%s historical median unavailable", market.Symbol)
			continue
		}
		if count < 2 {
			continue
		}
		// The configured bounds apply to the current spread minus the rolling median.
		deviation := spread - median
		marker, title := "", ""
		switch {
		case deviation > market.High:
			marker, title = "🟢", "Отклонение выше медианы"
		case deviation < market.Low:
			marker, title = "🔴", "Отклонение ниже медианы"
		default:
			continue
		}
		lastSentAt, err := b.lastInRangeNotification(ctx, market.Symbol)
		if err != nil {
			log.Printf("notification schedule read failed for %s", market.Symbol)
			continue
		}
		if now-lastSentAt >= 30 {
			message := formatSpreadNotification(marker, title, market, median, spread, deviation, core, rh)
			b.notifyAllHTML(ctx, message)
			if err := b.setLastInRangeNotification(ctx, market.Symbol, time.Now().Unix()); err != nil {
				log.Printf("notification schedule write failed for %s", market.Symbol)
			}
		}
	}
	return nil
}

func (b *Bot) marketMedian(ctx context.Context, market MarketConfig, coreID, rhID int, end int64) (float64, int, error) {
	b.medianCacheMu.Lock()
	if cached, ok := b.medianCache[market.Symbol]; ok && cached.end == end &&
		cached.coreMarket == market.CoreMarket && cached.rhMarket == market.RHMarket &&
		cached.coreMarketID == coreID && cached.rhMarketID == rhID && cached.window == market.Window {
		b.medianCacheMu.Unlock()
		return cached.median, cached.count, nil
	}
	b.medianCacheMu.Unlock()

	start := end - int64(market.Window/time.Second)
	coreCandles, err := b.fetchMarkPriceCandles(ctx, b.cfg.Lighter.CoreAPI, coreID, start, end)
	if err != nil {
		return 0, 0, err
	}
	rhCandles, err := b.fetchMarkPriceCandles(ctx, b.cfg.Lighter.RobinhoodAPI, rhID, start, end)
	if err != nil {
		return 0, 0, err
	}
	median, count := pairedCandleMedian(coreCandles, rhCandles, start*1000, end*1000)
	b.medianCacheMu.Lock()
	if b.medianCache == nil {
		b.medianCache = make(map[string]cachedMedian)
	}
	b.medianCache[market.Symbol] = cachedMedian{
		end: end, coreMarket: market.CoreMarket, rhMarket: market.RHMarket,
		coreMarketID: coreID, rhMarketID: rhID, window: market.Window,
		median: median, count: count,
	}
	b.medianCacheMu.Unlock()
	return median, count, nil
}

func pairedCandleMedian(coreCandles, rhCandles []markPriceCandle, startMS, endMS int64) (float64, int) {
	coreByTime := make(map[int64]float64, len(coreCandles))
	for _, candle := range coreCandles {
		if candle.Timestamp >= startMS && candle.Timestamp < endMS && candle.Close > 0 {
			coreByTime[candle.Timestamp] = candle.Close
		}
	}
	spreads := make([]float64, 0, len(coreByTime))
	for _, candle := range rhCandles {
		if candle.Timestamp < startMS || candle.Timestamp >= endMS || candle.Close <= 0 {
			continue
		}
		if core, ok := coreByTime[candle.Timestamp]; ok {
			spreads = append(spreads, (candle.Close-core)/core*100)
		}
	}
	if len(spreads) == 0 {
		return 0, 0
	}
	sort.Float64s(spreads)
	middle := len(spreads) / 2
	median := spreads[middle]
	if len(spreads)%2 == 0 {
		median = (spreads[middle-1] + spreads[middle]) / 2
	}
	return median, len(spreads)
}

func formatSpreadNotification(marker, title string, market MarketConfig, median, current, deviation, core, robinhood float64) string {
	return fmt.Sprintf("%s <b>%s · %s</b>\n• Медиана за %d мин.: %+.4f%%\n• Текущие цены: Core %.8g; Robinhood %.8g (спред %+.4f%%)\n• Отклонение от медианы: %+.4f п.п.", marker, html.EscapeString(title), html.EscapeString(market.Symbol), int(market.Window.Minutes()), median, core, robinhood, current, deviation)
}
