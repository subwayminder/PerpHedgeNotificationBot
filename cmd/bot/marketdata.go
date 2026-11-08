package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type market struct {
	MarketID       int             `json:"market_id"`
	Symbol         string          `json:"symbol"`
	LastTradePrice json.RawMessage `json:"last_trade_price"`
	MarkPrice      json.RawMessage `json:"mark_price"`
}

type markPriceCandle struct {
	Timestamp int64   `json:"t"`
	Close     float64 `json:"c"`
}

func (b *Bot) fetchMarket(ctx context.Context, baseURL, symbol string) (float64, error) {
	_, price, err := b.fetchMarketData(ctx, baseURL, symbol)
	return price, err
}

func (b *Bot) fetchMarketData(ctx context.Context, baseURL, symbol string) (int, float64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/v1/orderBookDetails", nil)
	if err != nil {
		return 0, 0, err
	}
	resp, err := b.client.Do(req)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, 0, fmt.Errorf("market API returned %s", resp.Status)
	}
	var payload struct {
		OK               *bool           `json:"ok"`
		Result           json.RawMessage `json:"result"`
		OrderBookDetails []market        `json:"order_book_details"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&payload); err != nil {
		return 0, 0, err
	}
	if payload.OK != nil && !*payload.OK {
		return 0, 0, errors.New("market API rejected request")
	}
	markets := payload.OrderBookDetails
	if len(markets) == 0 && len(payload.Result) > 0 {
		if err := json.Unmarshal(payload.Result, &markets); err != nil {
			var wrapped struct {
				OrderBookDetails []market `json:"order_book_details"`
			}
			if err := json.Unmarshal(payload.Result, &wrapped); err != nil {
				return 0, 0, err
			}
			markets = wrapped.OrderBookDetails
		}
	}
	for _, item := range markets {
		if strings.EqualFold(item.Symbol, symbol) {
			if item.MarketID <= 0 {
				return 0, 0, fmt.Errorf("invalid market id for %s", symbol)
			}
			price, err := decodePrice(item.MarkPrice)
			if err != nil {
				price, err = decodePrice(item.LastTradePrice)
			}
			if err != nil {
				return 0, 0, fmt.Errorf("invalid price for %s", symbol)
			}
			return item.MarketID, price, nil
		}
	}
	return 0, 0, fmt.Errorf("market %s not found", symbol)
}

func (b *Bot) fetchMarkPriceCandles(ctx context.Context, baseURL string, marketID int, start, end int64) ([]markPriceCandle, error) {
	const maxCandles = int64(500)
	const minute = int64(time.Minute / time.Second)
	var candles []markPriceCandle
	for chunkStart := start; chunkStart < end; {
		chunkEnd := chunkStart + maxCandles*minute
		if chunkEnd > end {
			chunkEnd = end
		}
		params := url.Values{
			"market_id":       {strconv.Itoa(marketID)},
			"resolution":      {"1m"},
			"start_timestamp": {strconv.FormatInt(chunkStart, 10)},
			"end_timestamp":   {strconv.FormatInt(chunkEnd, 10)},
			"count_back":      {strconv.FormatInt(maxCandles, 10)},
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/v1/markPriceCandles?"+params.Encode(), nil)
		if err != nil {
			return nil, err
		}
		resp, err := b.client.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("mark price candles API returned %s", resp.Status)
		}
		var payload struct {
			Code    int               `json:"code"`
			Candles []markPriceCandle `json:"c"`
		}
		err = json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&payload)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		if payload.Code != http.StatusOK {
			return nil, fmt.Errorf("mark price candles API returned code %d", payload.Code)
		}
		candles = append(candles, payload.Candles...)
		chunkStart = chunkEnd
	}
	return candles, nil
}

func decodePrice(raw json.RawMessage) (float64, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, errors.New("price unavailable")
	}
	price, err := strconv.ParseFloat(strings.Trim(string(raw), `"`), 64)
	if err != nil || price <= 0 {
		return 0, errors.New("invalid price")
	}
	return price, nil
}
