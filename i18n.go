package main

import (
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var messages = map[string]map[string]string{
	"ru": {
		"lang_switch":           "English",
		"empty":                 "Список пока пуст.",
		"gift_this":             "Подарю это",
		"taken":                 "Уже кто-то дарит",
		"yours":                 "Это дарите вы",
		"manage":                "управлять",
		"left":                  "Осталось %d из %d",
		"priority_high":         "Очень хочу",
		"go_store":              "Открыть в магазине",
		"back":                  "← К списку",
		"reserve_title":         "Подарить этот подарок?",
		"reserve_explain":       "Подарок будет отмечен как занятый: другие увидят, что его уже кто-то дарит, но не узнают кто. Имя и регистрация не нужны. Бронь действует %s. Если за это время не отметить на сайте, что подарок уже куплен, он снова станет доступен другим.",
		"email_label":           "Оставьте email — пришлём ссылку на вашу бронь",
		"email_hint":            "По этой ссылке вы потом отметите на сайте, что подарок уже куплен. Ещё одно письмо-напоминание придёт за 3 дня до конца брони. Адрес не видят ни владелец списка, ни другие гости, и больше он ни для чего не используется.",
		"reserve_confirm":       "Да, дарю",
		"res_title":             "Спасибо! Подарок за вами",
		"res_link_emailed":      "Мы отправили эту ссылку на ваш email. Когда купите подарок, обязательно откройте её и нажмите «Я купил(а)».",
		"res_save_link":         "Сохраните эту ссылку: по ней можно отметить, что подарок уже куплен, или отменить бронь. На этом устройстве бронь запомнится и так, но с другого без ссылки не получится.",
		"res_expires":           "Бронь действует до %s. Если не отметить, что подарок уже куплен, он снова станет доступен другим.",
		"res_ship_to":           "Куда отправить",
		"res_ship_own":          "Если собираетесь вручить подарок лично, закажите его на свой адрес. Если приехать в гости не получится, отправьте его на мой адрес ниже.",
		"res_ship_hint":         "Магазин в США — мой адрес в США, магазин в другой стране — мой адрес в Канаде. Выберите нужный:",
		"res_ship_fixed":        "Для этого подарка подходит только адрес: %s.",
		"region_US":             "США",
		"region_CA":             "Канада",
		"copy":                  "Копировать",
		"copied":                "Скопировано",
		"addr_name":             "Имя",
		"addr_line1":            "Адрес, строка 1",
		"addr_line2":            "Адрес, строка 2",
		"addr_city":             "Город",
		"addr_province":         "Провинция / штат",
		"addr_postal":           "Почтовый индекс",
		"addr_country":          "Страна",
		"copy_all":              "Копировать всё",
		"price_from":            "от",
		"shop_unavailable":      "Временно недоступен в магазине",
		"cond_short":            "Только Mint или NM",
		"cond_title":            "Покупайте только в состоянии Mint или Near Mint",
		"cond_intro":            "В объявлении на Discogs и Media Condition, и Sleeve Condition должны быть Mint (M) или Near Mint (NM или M-). Объявления с другими оценками (VG+, VG и ниже) не подходят.",
		"cond_media":            "сам носитель",
		"cond_sleeve":           "конверт, обложка, вкладыш",
		"cond_or":               "или",
		"cond_source":           "Описания оценок — по справке Discogs «How To Grade Items».",
		"ship_rules_title":      "Как доставить подарок",
		"ship_rule_own":         "Если собираетесь вручить лично, закажите подарок на свой адрес.",
		"ship_rule_remote":      "Если дарите на расстоянии, отправьте его на мой адрес: из магазина в США — на мой адрес в США, из магазина в любой другой стране — на мой адрес в Канаде. Адрес появится на следующем шаге.",
		"ship_rule_fixed":       "Если на следующем шаге выбрать адрес нельзя, значит, ссылка ведёт в магазин конкретной страны и подходящий адрес уже выбран.",
		"tap_to_copy":           "Нажмите на поле, чтобы скопировать его.",
		"mark_purchased":        "Я купил(а)",
		"purchased_done":        "Отмечено как купленное. Спасибо!",
		"cancel":                "Отменить бронь",
		"confirm_cancel":        "Точно отменить? Подарок снова станет доступен другим.",
		"cancelled":             "Бронь отменена.",
		"not_found":             "Ссылка недействительна: бронь отменена или истекла.",
		"item_not_found":        "Такого подарка в списке нет.",
		"unavailable":           "Увы, этот подарок только что забронировал кто-то другой.",
		"rate_limited":          "Слишком много попыток. Попробуйте через час.",
		"bad_email":             "Похоже, в адресе email опечатка.",
		"email_required":        "Укажите email: на него придёт ссылка на вашу бронь.",
		"no_address":            "Адрес пока не указан. Напишите владельцу списка.",
		"server_error":          "Что-то пошло не так. Попробуйте ещё раз.",
		"mail_confirm_subject":  "Вы выбрали подарок: %s",
		"mail_confirm_body":     "Здравствуйте!\n\nВы выбрали подарок в вишлисте %[2]s: %[1]s.\n\nОн отмечен за вами до %[3]s, поэтому другие гости его не выберут.\n\nНа странице подарка есть ссылка на магазин и адрес доставки. Там же можно отметить, что подарок куплен, тогда он останется за вами и после этой даты:\n%[4]s\n\nЭто письмо пришло, потому что вы указали свой адрес, когда выбирали подарок. Если подарок не будет отмечен купленным, придёт ещё одно напоминание за 3 дня до %[3]s. Других писем не будет.\n",
		"mail_reminder_subject": "Напоминание о подарке: %s",
		"mail_reminder_body":    "Здравствуйте!\n\nНесколько дней назад вы выбрали подарок в вишлисте %[2]s: %[1]s. Он отмечен за вами до %[3]s.\n\nЕсли вы его уже купили, отметьте это на странице подарка, и он останется за вами:\n%[4]s\n\nЕсли передумали, там же можно снять выбор, и подарок станет доступен другим.\n",
	},
	"en": {
		"lang_switch":           "Русский",
		"empty":                 "The list is empty for now.",
		"gift_this":             "I’ll gift this",
		"taken":                 "Someone is gifting this",
		"yours":                 "You’re gifting this",
		"manage":                "manage",
		"left":                  "%d of %d left",
		"priority_high":         "Really want",
		"go_store":              "Open in store",
		"back":                  "← Back to the list",
		"reserve_title":         "Gift this?",
		"reserve_explain":       "The gift will be marked as taken: others will see that someone is gifting it, but not who. No name or sign-up needed. The reservation lasts %s — if it is not marked as bought by then, the gift becomes available again.",
		"email_label":           "Leave your email — we’ll send you a link to your reservation",
		"email_hint":            "You will use this link later to mark the gift as bought. One more reminder comes 3 days before the reservation ends. Neither the list owner nor other guests see the address, and it is used for nothing else.",
		"reserve_confirm":       "Yes, I’ll gift it",
		"res_title":             "Thank you! The gift is yours",
		"res_link_emailed":      "We have emailed you this link. Once you have bought the gift, be sure to open it and press “I bought it”.",
		"res_save_link":         "Save this link: it lets you mark the gift as bought or cancel the reservation. This device remembers it anyway, but from another one you will need the link.",
		"res_expires":           "Reserved until %s. If it is not marked as bought, the gift becomes available to others again.",
		"res_ship_to":           "Where to ship",
		"res_ship_own":          "If you plan to hand the gift over in person, order it to your own address. If distance keeps you from visiting, ship it to my address below.",
		"res_ship_hint":         "US store — my US address; store in any other country — my Canadian address. Pick the one you need:",
		"res_ship_fixed":        "For this gift, only this address works: %s.",
		"region_US":             "USA",
		"region_CA":             "Canada",
		"copy":                  "Copy",
		"copied":                "Copied",
		"addr_name":             "Name",
		"addr_line1":            "Address Line 1",
		"addr_line2":            "Address Line 2",
		"addr_city":             "City",
		"addr_province":         "Province",
		"addr_postal":           "Postal Code",
		"addr_country":          "Country",
		"copy_all":              "Copy All",
		"price_from":            "from",
		"shop_unavailable":      "Temporarily unavailable in store",
		"cond_short":            "Mint or NM only",
		"cond_title":            "Buy only in Mint or Near Mint condition",
		"cond_intro":            "In the Discogs listing both Media Condition and Sleeve Condition must be Mint (M) or Near Mint (NM or M-). Listings with any other grade (VG+, VG or lower) will not do.",
		"cond_media":            "the record, disc or tape itself",
		"cond_sleeve":           "sleeve, cover, inserts",
		"cond_or":               "or",
		"cond_source":           "Grade definitions are from Discogs’ “How To Grade Items”.",
		"ship_rules_title":      "How to get the gift to me",
		"ship_rule_own":         "If you plan to hand it over in person, order it to your own address.",
		"ship_rule_remote":      "If you are gifting from afar, ship it to my address: from a US store to my US address, from a store in any other country to my Canadian address. You will see the address on the next step.",
		"ship_rule_fixed":       "If the next step offers no choice of address, the link leads to a store in a specific country and the right address is already chosen.",
		"tap_to_copy":           "Tap a field to copy it.",
		"mark_purchased":        "I bought it",
		"purchased_done":        "Marked as bought. Thank you!",
		"cancel":                "Cancel reservation",
		"confirm_cancel":        "Cancel for sure? The gift will become available to others.",
		"cancelled":             "Reservation cancelled.",
		"not_found":             "This link is no longer valid: the reservation was cancelled or has expired.",
		"item_not_found":        "There is no such gift on the list.",
		"unavailable":           "Sorry, someone else has just reserved this gift.",
		"rate_limited":          "Too many attempts. Please try again in an hour.",
		"bad_email":             "That email address looks mistyped.",
		"email_required":        "Please enter your email: the link to your reservation goes there.",
		"no_address":            "No address yet. Please contact the list owner.",
		"server_error":          "Something went wrong. Please try again.",
		"mail_confirm_subject":  "You picked a gift: %s",
		"mail_confirm_body":     "Hello!\n\nYou picked a gift on the wishlist at %[2]s: %[1]s.\n\nIt is kept for you until %[3]s, so other guests will not pick it.\n\nThe gift page has the store link and the shipping address. You can also mark the gift as bought there, and it stays yours after that date:\n%[4]s\n\nYou got this email because you left your address when picking the gift. If the gift is not marked as bought, one more reminder comes 3 days before %[3]s. There will be no other emails.\n",
		"mail_reminder_subject": "A reminder about your gift: %s",
		"mail_reminder_body":    "Hello!\n\nA few days ago you picked a gift on the wishlist at %[2]s: %[1]s. It is kept for you until %[3]s.\n\nIf you have already bought it, mark it on the gift page and it stays yours:\n%[4]s\n\nIf you changed your mind, you can release it there, and the gift becomes available to others.\n",
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

// itemPrice is the price as visitors see it. A Discogs master or release has
// no shop price, only the cheapest marketplace offer, so it reads "from
// $8.00"; a single listing has a fixed price.
func itemPrice(it *Item, lang string) string {
	if it.Unavailable {
		return tr(lang, "shop_unavailable")
	}
	p := formatPrice(it.Price, it.Currency)
	if p == "" {
		return ""
	}
	if u, err := url.Parse(it.URL); err == nil && isDiscogs(u) {
		if kind, _, ok := discogsRef(u); ok && kind != "listing" {
			return tr(lang, "price_from") + " " + p
		}
	}
	return p
}

// pluralRU picks the noun form for a count by the Unicode CLDR rules for
// Russian: one (1, 21, 101: «день»), few (2–4, 22–24: «дня»), many (0, 5–20,
// 11–14, 100: «дней»).
func pluralRU(n int, one, few, many string) string {
	if n < 0 {
		n = -n
	}
	switch {
	case n%10 == 1 && n%100 != 11:
		return one
	case n%10 >= 2 && n%10 <= 4 && (n%100 < 12 || n%100 > 14):
		return few
	}
	return many
}

// days is "14 дней" / "14 days" with the noun agreeing with the number.
func days(lang string, n int) string {
	if lang == "ru" {
		return strconv.Itoa(n) + " " + pluralRU(n, "день", "дня", "дней")
	}
	if n == 1 {
		return "1 day"
	}
	return strconv.Itoa(n) + " days"
}

func itoa(n int) string { return strconv.Itoa(n) }

var templateFuncs = template.FuncMap{
	"t":    tr,
	"tf":   tf,
	"date": formatDate,
	"days": days,
	"datetime": func(lang string, t *time.Time) string {
		if t == nil {
			return ""
		}
		return formatDate(lang, *t) + ", " + t.Local().Format("15:04")
	},
	"fetchnote": fetchNote,
	"region": func(lang, code string) string {
		return tr(lang, "region_"+code)
	},
}
