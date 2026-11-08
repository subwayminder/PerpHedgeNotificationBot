package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

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
		reply = "Бот сравнивает текущий спред с медианой за окно.\n\n/add SYMBOL CORE_MARKET RH_MARKET WINDOW_MIN LOW HIGH\nПример: /add ANTHROPIC ANTHROPIC ANTHROPIC 15 -0.5 0.5\n/list — показать настройки\n/remove SYMBOL — удалить актив\n\nLOW и HIGH — границы отклонения текущего спреда от медианы в процентных пунктах. Если отклонение внутри диапазона, бот присылает уведомление раз в минуту. Маркер 🔴/🟢 показывает знак текущего спреда. Это уведомление, не торговый совет."
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
		return "Использование: /add SYMBOL CORE_MARKET RH_MARKET WINDOW_MIN LOW% HIGH%"
	}
	symbol, core, rh := strings.ToUpper(parts[1]), strings.ToUpper(parts[2]), strings.ToUpper(parts[3])
	window, windowErr := strconv.Atoi(parts[4])
	low, lowErr := strconv.ParseFloat(parts[5], 64)
	high, highErr := strconv.ParseFloat(parts[6], 64)
	if len(symbol) > 32 || len(core) > 64 || len(rh) > 64 || window < 1 || window > 1440 || windowErr != nil || lowErr != nil || highErr != nil || low >= high || low < -100 || high > 100 {
		return "Некорректные параметры: окно 1–1440 минут, границы от -100 до 100 и LOW < HIGH."
	}
	if err := b.saveMarket(ctx, symbol, core, rh, window, low, high); err != nil {
		return "Не удалось сохранить настройки в SQLite"
	}
	return fmt.Sprintf("Настроено %s: окно %d мин., уведомления при отклонении текущего спреда от медианы в диапазоне [%.4f, %.4f] п.п. — не чаще раза в минуту. Проверьте, что рынки существуют на обеих площадках.", symbol, window, low, high)
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
