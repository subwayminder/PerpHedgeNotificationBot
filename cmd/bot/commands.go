package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

const maxWindowMinutes = 7 * 24 * 60

func (b *Bot) handleMessage(ctx context.Context, chatID, userID int64, text string) {
	if !b.authorized(ctx, chatID, userID) {
		_ = b.send(ctx, chatID, "Доступ запрещён. Добавьте ваш Telegram user ID в config.yaml и перезапустите бота.")
		return
	}
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return
	}
	var reply string
	switch strings.ToLower(strings.Split(parts[0], "@")[0]) {
	case "/start", "/help":
		reply = "Бот сравнивает текущий спред с медианой за окно.\n\n/add SYMBOL CORE_MARKET RH_MARKET WINDOW LOW HIGH\nПример: /add ANTHROPIC ANTHROPIC ANTHROPIC 1h -0.5 0.5\nОкно можно указать в минутах (например, 60) или с суффиксом: 1h, 1d, 1w. Максимум — 1w; медиана обновляется раз в минуту.\n/list — показать настройки\n/remove SYMBOL — удалить актив\n\nLOW и HIGH — нижняя и верхняя границы отклонения спреда от медианы в процентных пунктах. Уведомление приходит только при отклонении строго выше HIGH (🟢) или строго ниже LOW (🔴). Внутри диапазона и точно на границах уведомлений нет. Это уведомление, не торговый совет."
	case "/add":
		reply = b.addMarket(ctx, parts)
	case "/list":
		reply = b.listMarkets(ctx)
	case "/remove":
		if len(parts) != 2 {
			reply = "Использование: /remove SYMBOL"
		} else {
			removed, err := b.deleteMarket(ctx, strings.ToUpper(parts[1]))
			switch {
			case err != nil:
				reply = "Ошибка SQLite"
			case !removed:
				reply = "Актив не найден"
			default:
				reply = "Актив удалён"
			}
		}
	default:
		reply = "Неизвестная команда. Используйте /help"
	}
	_ = b.send(ctx, chatID, reply)
}

func (b *Bot) addMarket(ctx context.Context, parts []string) string {
	if len(parts) != 7 {
		return "Использование: /add SYMBOL CORE_MARKET RH_MARKET WINDOW LOW HIGH (например, 60, 1h, 1d или 1w)"
	}
	symbol, core, rh := strings.ToUpper(parts[1]), strings.ToUpper(parts[2]), strings.ToUpper(parts[3])
	window, validWindow := parseWindowMinutes(parts[4])
	low, lowErr := strconv.ParseFloat(parts[5], 64)
	high, highErr := strconv.ParseFloat(parts[6], 64)
	if len(symbol) > 32 || len(core) > 64 || len(rh) > 64 || !validWindow || lowErr != nil || highErr != nil || low >= high || low < -100 || high > 100 {
		return "Некорректные параметры: окно в минутах или формате Nh/Nd/Nw (максимум 1w), границы от -100 до 100 и LOW < HIGH."
	}
	if err := b.saveMarket(ctx, symbol, core, rh, window, low, high); err != nil {
		return "Не удалось сохранить настройки в SQLite"
	}
	return fmt.Sprintf("Настроено %s: окно %d мин. (максимум 10080 мин., обновление медианы раз в минуту), уведомления при отклонении строго выше %.4f п.п. или ниже %.4f п.п.; внутри диапазона уведомлений нет. Проверьте, что рынки существуют на обеих площадках.", symbol, window, high, low)
}

func parseWindowMinutes(input string) (int, bool) {
	value := strings.ToLower(strings.TrimSpace(input))
	factor := 1
	if len(value) > 0 {
		switch value[len(value)-1] {
		case 'm':
			value = value[:len(value)-1]
		case 'h':
			factor = 60
			value = value[:len(value)-1]
		case 'd':
			factor = 24 * 60
			value = value[:len(value)-1]
		case 'w':
			factor = 7 * 24 * 60
			value = value[:len(value)-1]
		}
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < 1 || n > maxWindowMinutes/factor {
		return 0, false
	}
	return n * factor, true
}

func (b *Bot) listMarkets(ctx context.Context) string {
	markets, err := b.markets(ctx)
	if err != nil {
		return "Ошибка чтения SQLite"
	}
	if len(markets) == 0 {
		return "Активов пока нет. Добавьте через /add."
	}
	var out strings.Builder
	out.WriteString("Активы:\n")
	for _, market := range markets {
		fmt.Fprintf(&out, "%s: Core=%s, Robinhood=%s, окно=%d мин., диапазон [%.4f%%, %.4f%%]\n", market.Symbol, market.CoreMarket, market.RHMarket, int(market.Window.Minutes()), market.Low, market.High)
	}
	return out.String()
}
