package main

import "strings"

// adminTexts are the admin panel's texts: key, Russian, English. They join
// messages at start-up; a key that already exists there is a bug.
var adminTexts = [][3]string{
	{"nav_gifts", "Подарки", "Gifts"},
	{"nav_settings", "Настройки", "Settings"},
	{"nav_site", "Сайт ↗", "Site ↗"},
	{"nav_logout", "Выйти", "Log out"},

	{"login_page", "Вход", "Sign in"},
	{"login_title", "Вход в админку", "Admin sign-in"},
	{"login_password", "Пароль", "Password"},
	{"login_submit", "Войти", "Sign in"},
	{"login_too_many", "Слишком много попыток. Подождите 15 минут.", "Too many attempts. Please wait 15 minutes."},
	{"login_wrong", "Неверный пароль.", "Wrong password."},

	{"msg_saved", "Сохранено.", "Saved."},
	{"msg_deleted", "Подарок удалён.", "Gift deleted."},
	{"msg_refreshed", "Данные с сайта обновлены.", "Updated from the shop."},
	{"msg_released", "Брони сняты.", "Reservations released."},
	{"msg_bad_url", "Нужна ссылка вида https://…", "The link must look like https://…"},

	{"status_active", "Активен", "Active"},
	{"status_received", "Получен", "Received"},
	{"status_owned", "Уже есть", "Already have"},
	{"region_auto", "авто", "auto"},
	{"adm_reserved", "%d из %d", "%d of %d"},
	{"adm_bought", ", куплено %d", ", %d bought"},

	{"add_placeholder", "Ссылка на товар: Amazon или любой другой магазин", "Link to the product: Amazon or any other shop"},
	{"add_button", "Добавить", "Add"},
	{"list_order_hint", "Гости видят подарки в том же порядке, что здесь, только забронированные уходят в конец. Перетащите строку за ⋮⋮, чтобы поменять порядок; перенос в верхний список ставит ★, в нижний — снимает.", "Guests see the gifts in the same order as here, except that reserved ones go to the end. Drag a row by ⋮⋮ to change the order; moving it to the top list sets ★, to the bottom list clears it."},
	{"list_sorted_hint", "Гости видят подарки в том же порядке, что здесь, только забронированные уходят в конец, отсортированные по тому же правилу. Если перетащить строку за ⋮⋮, порядок переключится на «Вручную» и останется таким, как сейчас, с вашей правкой.", "Guests see the gifts in the same order as here, except that reserved ones go to the end, sorted by the same rule. Dragging a row by ⋮⋮ switches the order to Manual, keeping the list as it is now plus your change."},
	{"sort_label", "Порядок", "Order"},
	{"group_hot", "★ Очень хочу", "★ Really want"},
	{"group_hot_empty", "Перетащите сюда подарок, чтобы отметить его «Очень хочу».", "Drag a gift here to mark it “Really want”."},
	{"group_rest", "Остальные", "Everything else"},
	{"sort_apply", "Применить", "Apply"},
	{"sort_", "Вручную (перетаскиванием)", "Manual (drag and drop)"},
	{"sort_date_desc", "Дата добавления: сначала новые", "Date added: newest first"},
	{"sort_date_asc", "Дата добавления: сначала старые", "Date added: oldest first"},
	{"sort_title_asc", "Название: A→Z, затем А→Я", "Title: A→Z, then А→Я"},
	{"sort_title_desc", "Название: Z→A, затем Я→А", "Title: Z→A, then Я→А"},
	{"sort_price_asc", "Цена: сначала дешёвые", "Price: lowest first"},
	{"sort_price_desc", "Цена: сначала дорогие", "Price: highest first"},
	{"sort_price_note", "Цены в разных валютах сравниваются в канадских долларах по дневному курсу Банка Канады. Подарки без цены и временно недоступные — в конце.", "Prices in different currencies are compared in Canadian dollars at the Bank of Canada daily rate. Gifts without a price and temporarily unavailable ones go last."},
	{"spoilers_on", "Брони видны: режим со спойлерами.", "Reservations are visible: spoiler mode."},
	{"spoilers_off", "Режим без спойлеров: брони скрыты здесь и в письмах-уведомлениях. Переключается в настройках.", "Spoiler-free mode: reservations are hidden here and in the notice emails. Switch it in the settings."},
	{"col_order", "Порядок", "Order"},
	{"col_item", "Товар", "Gift"},
	{"col_price", "Цена", "Price"},
	{"col_address", "Адрес", "Address"},
	{"col_qty", "Шт.", "Qty"},
	{"col_status", "Статус", "Status"},
	{"col_reservations", "Брони", "Reserved"},
	{"drag_title", "Перетащите, чтобы поменять порядок (или стрелки ↑↓)", "Drag to change the order (or use the ↑↓ keys)"},
	{"drag_label", "Переместить: %s", "Move: %s"},
	{"list_empty", "Пока пусто: вставьте первую ссылку выше.", "Nothing here yet: paste the first link above."},

	{"gift_page", "Подарок", "Gift"},
	{"back_all", "← Все подарки", "← All gifts"},
	{"f_url", "Ссылка на товар", "Product link"},
	{"f_title", "Название", "Title"},
	{"f_image", "Картинка (URL)", "Picture (URL)"},
	{"f_image_hint", "Если поменять ссылку, картинка скачается заново. Пустое поле убирает картинку.", "Change the link to download the picture again. An empty field removes the picture."},
	{"f_price", "Цена", "Price"},
	{"f_unavailable", "Магазин сейчас не продаёт товар: гости видят «Временно недоступен в магазине».", "The shop does not sell the item right now: guests see “Temporarily unavailable in store”."},
	{"f_price_checked", "С сайта: %s. Проверяется каждый день в 6:00.", "From the shop: %s. Checked every day at 6:00."},
	{"f_price_never", "С сайта пока не получена. Ежедневная проверка в 6:00 подставит её, если сайт отдаст цену.", "Not read from the shop yet. The daily check at 6:00 fills it in if the shop gives a price."},
	{"f_currency", "Валюта", "Currency"},
	{"f_quantity", "Количество", "Quantity"},
	{"f_region", "Куда доставлять", "Ship to"},
	{"f_region_auto", "Авто: гость выбирает, по умолчанию его страна", "Auto: the guest picks, their country by default"},
	{"f_region_us", "Только США (Shippsy)", "US only (Shippsy)"},
	{"f_region_ca", "Только Канада", "Canada only"},
	{"f_status", "Статус", "Status"},
	{"f_status_active", "Активен: виден гостям", "Active: guests see it"},
	{"f_status_received", "Получен: в архив", "Received: archived"},
	{"f_status_owned", "Уже есть: скрыть", "Already have: hidden"},
	{"f_notes", "Заметка для дарителей", "Note for gift-givers"},
	{"f_notes_hint", "Размер, цвет, «любой из вариантов», «можно б/у»…", "Size, colour, “any of these”, “used is fine”…"},
	{"f_priority", "★ Очень хочу", "★ Really want"},
	{"save", "Сохранить", "Save"},
	{"refresh_btn", "Обновить цену и картинку с сайта", "Update price and picture from the shop"},
	{"release_confirm", "Снять все брони с этого подарка? Дарители об этом не узнают.", "Release all reservations of this gift? The gift-givers will not be told."},
	{"release_btn", "Снять брони (%d)", "Release reservations (%d)"},
	{"delete_confirm", "Удалить подарок из списка?", "Delete the gift from the list?"},
	{"delete_btn", "Удалить", "Delete"},

	{"fn_read_failed", "Не удалось прочитать страницу магазина (%s). Заполните поля вручную.", "Could not read the shop's page (%s). Fill in the fields by hand."},
	{"fn_unavailable", "Магазин сейчас не продаёт этот товар: гости видят «Временно недоступен в магазине». Утренняя проверка в 6:00 подставит цену, когда товар появится.", "The shop does not sell this item right now: guests see “Temporarily unavailable in store”. The morning check at 6:00 fills in the price when the item is back."},
	{"fn_missing", "Со страницы не удалось взять: %s. Допишите вручную.", "Could not get from the page: %s. Fill it in by hand."},
	{"fn_image_failed", "Картинку скачать не удалось: %s.", "Could not download the picture: %s."},
	{"fn_image_bad_url", "Ссылка на картинку не похожа на https://…", "The picture link does not look like https://…"},
	{"miss_title", "название", "title"},
	{"miss_image", "картинка", "picture"},
	{"miss_price", "цена", "price"},

	{"settings_page", "Настройки", "Settings"},
	{"s_title_ru", "Заголовок (RU)", "Title (RU)"},
	{"s_title_en", "Заголовок (EN)", "Title (EN)"},
	{"s_intro_ru", "Приветствие (RU)", "Greeting (RU)"},
	{"s_intro_en", "Приветствие (EN)", "Greeting (EN)"},
	{"s_addresses", "Адреса доставки", "Shipping addresses"},
	{"s_addresses_hint", "Гость видит адрес только после бронирования, в этих же полях, и может скопировать каждое по отдельности или всё сразу. Postal Code в адресе США — это ZIP, с ним сервер открывает amazon.com, чтобы видеть цены.", "A guest sees the address only after reserving, in these same fields, and can copy each one or all at once. The Postal Code of the US address is the ZIP the server uses to open amazon.com and see prices."},
	{"addr_title_us", "США (Shippsy)", "USA (Shippsy)"},
	{"addr_title_ca", "Канада", "Canada"},
	{"s_reservations", "Брони", "Reservations"},
	{"s_reserve_days", "Срок брони, дней", "Reservation period, days"},
	{"s_spoilers", "Показывать, что забронировано (спойлеры) в админке и в письмах-уведомлениях", "Show what is reserved (spoilers) in the admin panel and in the notice emails"},
	{"s_notices", "Уведомления", "Notices"},
	{"s_notices_on", "Уведомления о бронях, покупках и отменах: письмом на %s.", "Notices about reservations, purchases and cancellations: by email to %s."},
	{"s_notices_off", "Уведомления о бронях, покупках и отменах: выключены, пока не настроена почта (SMTP_* в .env).", "Notices about reservations, purchases and cancellations: off until mail is set up (SMTP_* in .env)."},
	{"s_guest_mail_on", "Письма гостям: включены, отправитель %s.", "Emails to guests: on, sender %s."},
	{"s_guest_mail_off", "Письма гостям: выключены (SMTP_* в .env). Поле email у гостей скрыто.", "Emails to guests: off (SMTP_* in .env). Guests do not see the email field."},
	{"s_test_to", "Кому", "To"},
	{"s_test_placeholder", "ваш личный адрес, например …@gmail.com", "your personal address, e.g. …@gmail.com"},
	{"s_test_btn", "Отправить тестовое письмо", "Send test email"},
	{"s_test_hint", "Придёт то же письмо, что гость получает после брони, с примером подарка.", "You get the same email a guest gets after reserving, with a sample gift."},
	{"s_unset", "не задан", "not set"},
	{"mail_err", "Почта: %s", "Mail: %s"},
	{"mail_err_off", "SMTP_HOST или SMTP_FROM не заданы", "SMTP_HOST or SMTP_FROM is not set"},
	{"mail_err_bad_to", "адрес получателя не похож на email", "the recipient does not look like an email address"},
	{"mail_test_ok", "Сервер %s принял тестовое письмо для %s. Это то же письмо, что гость получает после брони, с примером подарка; ссылка в нём ведёт на главную страницу. Проверьте ящик, в том числе папку «Спам».", "%s accepted the test email for %s. It is the same email a guest gets after reserving, with a sample gift; its link goes to the home page. Check the inbox, including the spam folder."},
	{"sample_gift", "Пример подарка", "Sample gift"},
	{"s_notice_lang", "Язык писем-уведомлений", "Language of the notice emails"},

	// Owner's notice emails; the _hidden texts are for spoiler-free mode.
	{"notice_subject", "Вишлист: %s", "Wishlist: %s"},
	{"notice_reserved_subj", "подарок забронирован", "gift reserved"},
	{"notice_reserved", "Забронирован подарок: %s", "Gift reserved: %s"},
	{"notice_reserved_hidden", "Кто-то забронировал подарок из вашего списка. Какой именно, не показывается: включён режим без спойлеров.", "Someone reserved a gift from your list. Which one is not shown: spoiler-free mode is on."},
	{"notice_purchased_subj", "подарок куплен", "gift bought"},
	{"notice_purchased", "Подарок куплен: %s", "Gift bought: %s"},
	{"notice_purchased_hidden", "Один из подарков отмечен купленным.", "One of the gifts was marked as bought."},
	{"notice_cancelled_subj", "бронь отменена", "reservation cancelled"},
	{"notice_cancelled", "Бронь отменена: %s", "Reservation cancelled: %s"},
	{"notice_cancelled_hidden", "Одну бронь отменили, подарок снова доступен.", "A reservation was cancelled; the gift is available again."},
	{"notice_expired_subj", "бронь истекла", "reservation expired"},
	{"notice_expired", "Бронь истекла, подарок снова доступен: %s", "A reservation expired; the gift is available again: %s"},
	{"notice_expired_hidden", "Одна бронь истекла, подарок снова доступен.", "A reservation expired; the gift is available again."},
}

func init() {
	for _, m := range adminTexts {
		if _, dup := messages["en"][m[0]]; dup {
			panic("i18n: admin key " + m[0] + " is already in messages")
		}
		messages["ru"][m[0]] = m[1]
		messages["en"][m[0]] = m[2]
	}
}

// FetchInfo records what reading a shop's page could not fill in.
type FetchInfo struct {
	ReadErr     string   `json:"read_err,omitempty"`
	Unavailable bool     `json:"unavailable,omitempty"`
	Missing     []string `json:"missing,omitempty"` // "title", "image", "price"
	ImageErr    string   `json:"image_err,omitempty"`
	ImageBadURL bool     `json:"image_bad_url,omitempty"`
}

// fetchInfo is nil when the page gave everything.
func fetchInfo(m Meta, err error) *FetchInfo {
	if err != nil {
		return &FetchInfo{ReadErr: err.Error()}
	}
	if f := (&FetchInfo{Unavailable: m.Unavailable, Missing: m.missing()}); f.Unavailable || len(f.Missing) > 0 {
		return f
	}
	return nil
}

// withImageErr adds a failed picture download to f, which may be nil.
func withImageErr(f *FetchInfo, err error) *FetchInfo {
	if f == nil {
		f = &FetchInfo{}
	}
	f.ImageErr = err.Error()
	return f
}

// fetchNote explains f in the edit form, in the admin's language.
func fetchNote(lang string, f *FetchInfo) string {
	if f == nil {
		return ""
	}
	var notes []string
	if f.ReadErr != "" {
		notes = append(notes, tf(lang, "fn_read_failed", f.ReadErr))
	}
	if f.Unavailable {
		notes = append(notes, tr(lang, "fn_unavailable"))
	}
	if len(f.Missing) > 0 {
		names := make([]string, len(f.Missing))
		for i, k := range f.Missing {
			names[i] = tr(lang, "miss_"+k)
		}
		notes = append(notes, tf(lang, "fn_missing", strings.Join(names, ", ")))
	}
	if f.ImageBadURL {
		notes = append(notes, tr(lang, "fn_image_bad_url"))
	}
	if f.ImageErr != "" {
		notes = append(notes, tf(lang, "fn_image_failed", f.ImageErr))
	}
	return strings.Join(notes, " ")
}
