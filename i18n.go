package main

import (
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"
)

var messages = map[string]map[string]string{
	"ru": {
		"lang_switch":       "English",
		"empty":             "Список пока пуст.",
		"gift_this":         "Подарю это",
		"taken":             "Уже кто-то дарит",
		"yours":             "Это дарите вы",
		"manage":            "управлять",
		"left":              "Осталось %d из %d",
		"priority_high":     "Очень хочу",
		"go_store":          "Открыть в магазине",
		"back":              "← К списку",
		"reserve_title":     "Подарить этот подарок?",
		"reserve_explain":   "Подарок будет отмечен как занятый: другие увидят, что его уже кто-то дарит, но не узнают кто. Имя и регистрация не нужны. Бронь действует %d дн. — если за это время не отметить покупку, подарок снова станет доступен.",
		"email_label":       "Email (необязательно)",
		"email_hint":        "Пришлём ссылку на бронь и напомним за 3 дня до окончания. Больше ни для чего не используется.",
		"reserve_confirm":   "Да, дарю",
		"res_title":         "Спасибо! Подарок за вами",
		"res_save_link":     "Сохраните эту ссылку: по ней можно отметить покупку или отменить бронь. На этом устройстве бронь запомнится и так, но с другого без ссылки не получится.",
		"res_expires":       "Бронь действует до %s. Если не отметить покупку, подарок снова станет доступен другим.",
		"res_ship_to":       "Куда отправить",
		"res_ship_hint":     "Адрес выбран по магазину. Если заказываете доставку в другую страну, переключите.",
		"region_US":         "США",
		"region_CA":         "Канада",
		"copy":              "Копировать",
		"copied":            "Скопировано",
		"mark_purchased":    "Я купил(а)",
		"purchased_done":    "Отмечено как купленное. Спасибо!",
		"cancel":            "Отменить бронь",
		"confirm_cancel":    "Точно отменить? Подарок снова станет доступен другим.",
		"cancelled":         "Бронь отменена.",
		"not_found":         "Ссылка недействительна: бронь отменена или истекла.",
		"item_not_found":    "Такого подарка в списке нет.",
		"unavailable":       "Увы, этот подарок только что забронировал кто-то другой.",
		"rate_limited":      "Слишком много попыток. Попробуйте через час.",
		"bad_email":         "Похоже, в адресе email опечатка.",
		"no_address":        "Адрес пока не указан. Напишите владельцу списка.",
		"server_error":      "Что-то пошло не так. Попробуйте ещё раз.",
		"mail_confirm_subject": "Ваша бронь подарка",
		"mail_confirm_body": "Вы забронировали подарок: %s\n\nБронь действует до %s.\nАдрес доставки, отметка «купил» и отмена брони — по ссылке:\n%s\n",
		"mail_reminder_subject": "Бронь подарка скоро истечёт",
		"mail_reminder_body": "Напоминаем: бронь подарка «%s» истекает %s.\nЕсли вы его уже купили, отметьте это по ссылке, иначе подарок снова станет доступен другим:\n%s\n",
	},
	"en": {
		"lang_switch":       "Русский",
		"empty":             "The list is empty for now.",
		"gift_this":         "I’ll gift this",
		"taken":             "Someone is gifting this",
		"yours":             "You’re gifting this",
		"manage":            "manage",
		"left":              "%d of %d left",
		"priority_high":     "Really want",
		"go_store":          "Open in store",
		"back":              "← Back to the list",
		"reserve_title":     "Gift this?",
		"reserve_explain":   "The gift will be marked as taken: others will see that someone is gifting it, but not who. No name or sign-up needed. The reservation lasts %d days — if it is not marked as bought by then, the gift becomes available again.",
		"email_label":       "Email (optional)",
		"email_hint":        "We’ll send you the reservation link and a reminder 3 days before it expires. Used for nothing else.",
		"reserve_confirm":   "Yes, I’ll gift it",
		"res_title":         "Thank you! The gift is yours",
		"res_save_link":     "Save this link: it lets you mark the gift as bought or cancel the reservation. This device remembers it anyway, but from another one you will need the link.",
		"res_expires":       "Reserved until %s. If it is not marked as bought, the gift becomes available to others again.",
		"res_ship_to":       "Where to ship",
		"res_ship_hint":     "The address is chosen by the store. If your order ships to another country, switch it.",
		"region_US":         "USA",
		"region_CA":         "Canada",
		"copy":              "Copy",
		"copied":            "Copied",
		"mark_purchased":    "I bought it",
		"purchased_done":    "Marked as bought. Thank you!",
		"cancel":            "Cancel reservation",
		"confirm_cancel":    "Cancel for sure? The gift will become available to others.",
		"cancelled":         "Reservation cancelled.",
		"not_found":         "This link is no longer valid: the reservation was cancelled or has expired.",
		"item_not_found":    "There is no such gift on the list.",
		"unavailable":       "Sorry, someone else has just reserved this gift.",
		"rate_limited":      "Too many attempts. Please try again in an hour.",
		"bad_email":         "That email address looks mistyped.",
		"no_address":        "No address yet. Please contact the list owner.",
		"server_error":      "Something went wrong. Please try again.",
		"mail_confirm_subject": "Your gift reservation",
		"mail_confirm_body": "You reserved a gift: %s\n\nThe reservation lasts until %s.\nShipping address, “I bought it” and cancelling are at:\n%s\n",
		"mail_reminder_subject": "Your gift reservation expires soon",
		"mail_reminder_body": "A reminder: your reservation of “%s” expires on %s.\nIf you have already bought it, mark it at the link below, otherwise the gift becomes available to others:\n%s\n",
	},
}

func tr(lang, key string) string {
	if s, ok := messages[lang][key]; ok {
		return s
	}
	if s, ok := messages["en"][key]; ok {
		return s
	}
	return key
}

func tf(lang, key string, args ...any) string { return fmt.Sprintf(tr(lang, key), args...) }

func detectLang(r *http.Request) string {
	if c, err := r.Cookie("lang"); err == nil && (c.Value == "ru" || c.Value == "en") {
		return c.Value
	}
	for _, part := range strings.Split(r.Header.Get("Accept-Language"), ",") {
		tag, _, _ := strings.Cut(strings.TrimSpace(strings.ToLower(part)), ";")
		switch {
		case strings.HasPrefix(tag, "ru"):
			return "ru"
		case strings.HasPrefix(tag, "en"):
			return "en"
		}
	}
	return "en"
}

var monthsRU = []string{"января", "февраля", "марта", "апреля", "мая", "июня", "июля", "августа", "сентября", "октября", "ноября", "декабря"}

func formatDate(lang string, t time.Time) string {
	t = t.Local()
	if lang == "ru" {
		return fmt.Sprintf("%d %s %d", t.Day(), monthsRU[t.Month()-1], t.Year())
	}
	return t.Format("January 2, 2006")
}

var currencySymbols = map[string]string{"USD": "$", "CAD": "CA$", "EUR": "€", "GBP": "£"}

func formatPrice(price, currency string) string {
	if price == "" {
		return ""
	}
	if sym, ok := currencySymbols[currency]; ok {
		return sym + price
	}
	return strings.TrimSpace(price + " " + currency)
}

func itoa(n int) string { return strconv.Itoa(n) }

var templateFuncs = template.FuncMap{
	"t":    tr,
	"tf":   tf,
	"date": formatDate,
	"region": func(lang, code string) string {
		return tr(lang, "region_"+code)
	},
}
