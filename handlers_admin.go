package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/mail"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var adminMessages = map[string]string{
	"saved":     "Сохранено.",
	"deleted":   "Подарок удалён.",
	"refreshed": "Данные с сайта обновлены.",
	"released":  "Брони сняты.",
	"tg_ok":     "Тестовое сообщение отправлено в Telegram.",
	"mail_ok":   "Тестовое письмо отправлено на адрес отправителя. Проверьте этот ящик в iCloud.",
	"bad_url":   "Нужна ссылка вида https://…",
}

var statusNames = map[string]string{
	statusActive:   "Активен",
	statusReceived: "Получен",
	statusOwned:    "Уже есть",
}

func (a *App) adminBase(r *http.Request, title string) base {
	b := a.base(r, "ru", title)
	b.Admin = true
	if m := adminMessages[r.URL.Query().Get("m")]; m != "" {
		b.Flash = m
	}
	return b
}

func redirectMsg(w http.ResponseWriter, r *http.Request, path, msg string) {
	http.Redirect(w, r, path+"?m="+url.QueryEscape(msg), http.StatusSeeOther)
}

// ---- login ----

type loginPage struct{ base }

func (a *App) loginBase(r *http.Request) base {
	b := a.adminBase(r, "Вход")
	b.NoNav = true
	return b
}

func (a *App) handleLoginForm(w http.ResponseWriter, r *http.Request) {
	if a.isAdmin(r) {
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}
	a.render(w, http.StatusOK, "login", loginPage{a.loginBase(r)})
}

func (a *App) handleLogin(w http.ResponseWriter, r *http.Request) {
	p := loginPage{a.loginBase(r)}
	if !a.loginLimit.allow(a.clientIP(r)) {
		p.Error = "Слишком много попыток. Подождите 15 минут."
		a.render(w, http.StatusTooManyRequests, "login", p)
		return
	}
	if !a.adminHash.verify(r.FormValue("password")) {
		p.Error = "Неверный пароль."
		a.render(w, http.StatusUnauthorized, "login", p)
		return
	}
	exp := time.Now().Add(sessionMaxAge)
	http.SetCookie(w, &http.Cookie{
		Name: adminCookie, Value: a.signSession(exp), Path: "/",
		Expires: exp, HttpOnly: true, Secure: isHTTPS(r), SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (a *App) handleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: adminCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: isHTTPS(r)})
	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}

// ---- list ----

type adminRow struct {
	ID, Title, Image, PriceText, Site, RegionText, StatusText, Status, ReservedText string
	Priority, Quantity                                                              int
}

type adminPage struct {
	base
	Rows             []adminRow
	ShowReservations bool
}

func regionText(code string) string {
	switch code {
	case "US":
		return "США"
	case "CA":
		return "Канада"
	}
	return "авто"
}

func (a *App) handleAdmin(w http.ResponseWriter, r *http.Request) {
	p := adminPage{base: a.adminBase(r, "Подарки")}
	now := time.Now()
	a.store.view(func(d *Data) {
		p.ShowReservations = d.Settings.ShowReservations
		for _, it := range d.Items {
			row := adminRow{
				ID: it.ID, Title: it.Title, Image: it.Image,
				PriceText:  itemPrice(it, "ru"),
				Site:       siteName(it.URL),
				RegionText: regionText(it.Region),
				StatusText: statusNames[it.Status],
				Status:     it.Status,
				Priority:   it.Priority,
				Quantity:   it.Quantity,
			}
			if live, bought := it.reserved(now); live > 0 {
				row.ReservedText = fmt.Sprintf("%d из %d", live, it.Quantity)
				if bought > 0 {
					row.ReservedText += fmt.Sprintf(", куплено %d", bought)
				}
			}
			p.Rows = append(p.Rows, row)
		}
	})
	a.render(w, http.StatusOK, "admin", p)
}

// handleOrder saves the order the admin dragged the rows into.
func (a *App) handleOrder(w http.ResponseWriter, r *http.Request) {
	ids := strings.Split(r.FormValue("ids"), ",")
	if len(ids) == 0 || len(ids) > 1000 {
		http.Error(w, "bad ids", http.StatusBadRequest)
		return
	}
	if err := a.store.update(func(d *Data) error {
		d.reorder(ids)
		return nil
	}); err != nil {
		log.Printf("order: %v", err)
		http.Error(w, "save failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- add / edit ----

func parseShopURL(raw string) (*url.URL, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, false
	}
	return u, true
}

// fetchNote explains in the edit form what the page fetch could not fill in.
func fetchNote(m Meta, err error) string {
	if err != nil {
		return "Не удалось прочитать страницу магазина (" + err.Error() + "). Заполните поля вручную."
	}
	if miss := m.missing(); len(miss) > 0 {
		return "Со страницы не удалось взять: " + strings.Join(miss, ", ") + ". Допишите вручную."
	}
	return ""
}

// saveImage downloads src into the images dir and returns the new file name.
func (a *App) saveImage(ctx context.Context, itemID, src, referer string) (string, error) {
	b, ext, err := downloadImage(ctx, a.client, src, referer)
	if err != nil {
		return "", err
	}
	name := itemID + "-" + newID() + ext
	if err := os.WriteFile(filepath.Join(a.imagesDir, name), b, 0o644); err != nil {
		return "", err
	}
	return name, nil
}

func (a *App) removeImage(name string) {
	if name == "" || !reImageName.MatchString(name) {
		return
	}
	if err := os.Remove(filepath.Join(a.imagesDir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("remove image: %v", err)
	}
}

func (a *App) handleAdd(w http.ResponseWriter, r *http.Request) {
	u, ok := parseShopURL(r.FormValue("url"))
	if !ok {
		redirectMsg(w, r, "/admin", "bad_url")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	meta, ferr := a.fetchMeta(ctx, u.String())
	if ferr != nil {
		log.Printf("fetch %s: %v", u.Host, ferr)
	}
	now := time.Now()
	it := &Item{
		ID: newID(), URL: u.String(),
		Title: meta.Title, ImageSrc: meta.Image, Price: meta.Price, Currency: meta.Currency,
		Region: detectRegion(u), Quantity: 1, Status: statusActive,
		FetchNote: fetchNote(meta, ferr), CreatedAt: now, UpdatedAt: now,
	}
	if it.Region == "" {
		it.Region = detectRegion(meta.FinalURL) // short links such as a.co
	}
	if meta.Price != "" {
		it.PriceCheckedAt = &now
	}
	if it.Title == "" {
		it.Title = siteName(u.String())
	}
	if meta.Image != "" {
		name, err := a.saveImage(ctx, it.ID, meta.Image, u.String())
		if err != nil {
			log.Printf("image %s: %v", u.Host, err)
			it.FetchNote = strings.TrimSpace(it.FetchNote + " Картинку скачать не удалось: " + err.Error() + ".")
		}
		it.Image = name
	}
	if err := a.store.update(func(d *Data) error {
		d.Items = append([]*Item{it}, d.Items...) // new items go on top
		return nil
	}); err != nil {
		log.Printf("add: %v", err)
		http.Error(w, "save failed", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin/items/"+it.ID, http.StatusSeeOther)
}

type editPage struct {
	base
	Item             Item
	Reservations     int
	ShowReservations bool
}

func (a *App) handleEditForm(w http.ResponseWriter, r *http.Request) {
	p := editPage{base: a.adminBase(r, "Подарок")}
	var found bool
	a.store.view(func(d *Data) {
		if it := d.item(r.PathValue("id")); it != nil {
			found = true
			p.Item = *it
			p.Item.Reservations = nil
			p.Reservations = len(it.Reservations)
			p.ShowReservations = d.Settings.ShowReservations
		}
	})
	if !found {
		http.NotFound(w, r)
		return
	}
	p.PageTitle = p.Item.Title
	a.render(w, http.StatusOK, "edit", p)
}

func limit(s string, n int) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

func (a *App) handleEdit(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	u, ok := parseShopURL(r.FormValue("url"))
	if !ok {
		redirectMsg(w, r, "/admin/items/"+id, "bad_url")
		return
	}
	title := limit(r.FormValue("title"), 300)
	if title == "" {
		title = siteName(u.String())
	}
	qty, err := strconv.Atoi(r.FormValue("quantity"))
	if err != nil || qty < 1 {
		qty = 1
	}
	if qty > 99 {
		qty = 99
	}
	region := r.FormValue("region")
	if region != "US" && region != "CA" {
		region = ""
	}
	status := r.FormValue("status")
	if _, ok := statusNames[status]; !ok {
		status = statusActive
	}
	priority := 0
	if r.FormValue("priority") == "1" {
		priority = 1
	}
	imageSrc := strings.TrimSpace(r.FormValue("image_src"))

	var oldSrc string
	var exists bool
	a.store.view(func(d *Data) {
		if it := d.item(id); it != nil {
			oldSrc, exists = it.ImageSrc, true
		}
	})
	if !exists {
		http.NotFound(w, r)
		return
	}

	// A changed picture URL is downloaded before taking the store lock.
	newImage, imageChanged, imageNote := "", imageSrc != oldSrc, ""
	if imageChanged && imageSrc != "" {
		if _, ok := parseShopURL(imageSrc); !ok {
			imageNote = "Ссылка на картинку не похожа на https://…"
			imageChanged = false
		} else {
			ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
			name, err := a.saveImage(ctx, id, imageSrc, u.String())
			cancel()
			if err != nil {
				imageNote = "Картинку скачать не удалось: " + err.Error() + "."
				imageChanged = false
			}
			newImage = name
		}
	}

	var oldImage string
	err = a.store.update(func(d *Data) error {
		it := d.item(id)
		if it == nil {
			return errNotFound
		}
		it.URL = u.String()
		it.Title = title
		it.Price = limit(r.FormValue("price"), 32)
		it.Currency = strings.ToUpper(limit(r.FormValue("currency"), 3))
		it.Region = region
		it.Quantity = qty
		it.Status = status
		it.Priority = priority
		it.Notes = limit(r.FormValue("notes"), 1000)
		it.FetchNote = imageNote
		if imageChanged {
			oldImage = it.Image
			it.Image, it.ImageSrc = newImage, imageSrc
		}
		if status != statusActive {
			for _, res := range it.Reservations {
				res.Email = ""
			}
		}
		it.UpdatedAt = time.Now()
		return nil
	})
	if errors.Is(err, errNotFound) {
		a.removeImage(newImage)
		http.NotFound(w, r)
		return
	}
	if err != nil {
		log.Printf("edit: %v", err)
		http.Error(w, "save failed", http.StatusInternalServerError)
		return
	}
	a.removeImage(oldImage)
	redirectMsg(w, r, "/admin/items/"+id, "saved")
}

func (a *App) handleRefresh(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var pageURL string
	a.store.view(func(d *Data) {
		if it := d.item(id); it != nil {
			pageURL = it.URL
		}
	})
	if pageURL == "" {
		http.NotFound(w, r)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	meta, ferr := a.fetchMeta(ctx, pageURL)
	note := fetchNote(meta, ferr)
	var newImage string
	if meta.Image != "" {
		name, err := a.saveImage(ctx, id, meta.Image, pageURL)
		if err != nil {
			note = strings.TrimSpace(note + " Картинку скачать не удалось: " + err.Error() + ".")
		}
		newImage = name
	}
	var oldImage string
	err := a.store.update(func(d *Data) error {
		it := d.item(id)
		if it == nil {
			return errNotFound
		}
		if meta.Price != "" {
			it.Price, it.Currency = meta.Price, meta.Currency
			now := time.Now()
			it.PriceCheckedAt = &now
		}
		if newImage != "" {
			oldImage = it.Image
			it.Image, it.ImageSrc = newImage, meta.Image
		}
		// Replace the placeholder (the shop's host name) set when nothing could be read.
		if meta.Title != "" && (it.Title == "" || it.Title == siteName(it.URL)) {
			it.Title = meta.Title
		}
		it.FetchNote = note
		it.UpdatedAt = time.Now()
		return nil
	})
	if err != nil {
		a.removeImage(newImage)
		if errors.Is(err, errNotFound) {
			http.NotFound(w, r)
			return
		}
		log.Printf("refresh: %v", err)
		http.Error(w, "save failed", http.StatusInternalServerError)
		return
	}
	a.removeImage(oldImage)
	redirectMsg(w, r, "/admin/items/"+id, "refreshed")
}

func (a *App) handleRelease(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	err := a.store.update(func(d *Data) error {
		it := d.item(id)
		if it == nil {
			return errNotFound
		}
		it.Reservations = nil
		return nil
	})
	if err != nil {
		http.NotFound(w, r)
		return
	}
	redirectMsg(w, r, "/admin/items/"+id, "released")
}

func (a *App) handleDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var image string
	err := a.store.update(func(d *Data) error {
		for i, it := range d.Items {
			if it.ID == id {
				image = it.Image
				d.Items = append(d.Items[:i], d.Items[i+1:]...)
				return nil
			}
		}
		return errNotFound
	})
	if err != nil {
		http.NotFound(w, r)
		return
	}
	a.removeImage(image)
	redirectMsg(w, r, "/admin", "deleted")
}

// ---- settings ----

type settingsPage struct {
	base
	Settings        Settings
	Addresses       []addressForm
	TelegramEnabled bool
	MailEnabled     bool
	MailFrom        string
	BaseURL         string
}

func (a *App) handleSettingsForm(w http.ResponseWriter, r *http.Request) {
	p := settingsPage{
		base:            a.adminBase(r, "Настройки"),
		TelegramEnabled: a.tg.enabled(),
		MailEnabled:     a.mail.enabled(),
		BaseURL:         a.cfg.BaseURL,
	}
	if e := r.URL.Query().Get("tgerr"); e != "" {
		p.Error = "Telegram: " + e
	}
	if e := r.URL.Query().Get("mailerr"); e != "" {
		p.Error = "Почта: " + e
	}
	p.MailFrom = a.cfg.SMTPFrom
	a.store.view(func(d *Data) { p.Settings = d.Settings })
	p.Addresses = []addressForm{
		{Prefix: "us", Title: "США (Shippsy)", Fields: p.Settings.AddrUS.fields()},
		{Prefix: "ca", Title: "Канада", Fields: p.Settings.AddrCA.fields()},
	}
	a.render(w, http.StatusOK, "settings", p)
}

// addressForm is one country's block of address inputs, named <prefix>_<key>.
type addressForm struct {
	Prefix, Title string
	Fields        []addrField
}

func addressFromForm(r *http.Request, prefix string) Address {
	v := func(key string) string { return limit(r.FormValue(prefix+"_"+key), 200) }
	return Address{
		Name: v("name"), Line1: v("line1"), Line2: v("line2"), City: v("city"),
		Province: v("province"), PostalCode: v("postal"), Country: v("country"),
	}
}

func (a *App) handleSettings(w http.ResponseWriter, r *http.Request) {
	days, err := strconv.Atoi(r.FormValue("reserve_days"))
	if err != nil || days < 1 {
		days = defaultReserveDays
	}
	if days > 90 {
		days = 90
	}
	s := Settings{
		TitleRU:          limit(r.FormValue("title_ru"), 100),
		TitleEN:          limit(r.FormValue("title_en"), 100),
		IntroRU:          limit(r.FormValue("intro_ru"), 2000),
		IntroEN:          limit(r.FormValue("intro_en"), 2000),
		AddrUS:           addressFromForm(r, "us"),
		AddrCA:           addressFromForm(r, "ca"),
		ShowReservations: r.FormValue("show_reservations") == "1",
		ReserveDays:      days,
	}
	if err := a.store.update(func(d *Data) error {
		d.Settings = s
		return nil
	}); err != nil {
		log.Printf("settings: %v", err)
		http.Error(w, "save failed", http.StatusInternalServerError)
		return
	}
	redirectMsg(w, r, "/admin/settings", "saved")
}

func (a *App) handleTelegramTest(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if err := a.tg.sendSync(ctx, "🎁 Тест: уведомления вишлиста работают."); err != nil {
		http.Redirect(w, r, "/admin/settings?tgerr="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	redirectMsg(w, r, "/admin/settings", "tg_ok")
}

// handleMailTest sends one message to the sender's own address: it proves the
// SMTP login, encryption and sending address in one go.
func (a *App) handleMailTest(w http.ResponseWriter, r *http.Request) {
	if !a.mail.enabled() {
		http.Redirect(w, r, "/admin/settings?mailerr="+url.QueryEscape("SMTP_HOST или SMTP_FROM не заданы"), http.StatusSeeOther)
		return
	}
	from, err := mail.ParseAddress(a.cfg.SMTPFrom)
	if err == nil {
		err = a.mail.send(from.Address, "Wishlist: тестовое письмо", "Это тестовое письмо вишлиста. Если вы его читаете, отправка работает.\n\n"+a.cfg.BaseURL+"\n")
	}
	if err != nil {
		log.Printf("mail test: %v", err)
		http.Redirect(w, r, "/admin/settings?mailerr="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	redirectMsg(w, r, "/admin/settings", "mail_ok")
}
