package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"net/mail"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

var validStatus = map[string]bool{statusActive: true, statusReceived: true, statusOwned: true}

// adminBase is the page frame in the admin's language; titleKey is a message key.
func (a *App) adminBase(r *http.Request, titleKey string) base {
	lang := detectLang(r)
	b := a.base(r, lang, tr(lang, titleKey))
	b.Admin = true
	// ?m= names one of the msg_* texts after a redirect.
	if m := r.URL.Query().Get("m"); m != "" {
		if _, ok := messages["en"]["msg_"+m]; ok {
			b.Flash = tr(lang, "msg_"+m)
		}
	}
	return b
}

func redirectMsg(w http.ResponseWriter, r *http.Request, path, msg string) {
	http.Redirect(w, r, path+"?m="+url.QueryEscape(msg), http.StatusSeeOther)
}

// ---- login ----

type loginPage struct{ base }

func (a *App) loginBase(r *http.Request) base {
	b := a.adminBase(r, "login_page")
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
		p.Error = tr(p.Lang, "login_too_many")
		a.render(w, http.StatusTooManyRequests, "login", p)
		return
	}
	if !a.adminHash.verify(r.FormValue("password")) {
		p.Error = tr(p.Lang, "login_wrong")
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
	HotRows    []adminRow // ★ "really want", shown as their own list on top
	Rows       []adminRow
	SortOrder  string
	SortOrders []string
}

func regionText(lang, code string) string {
	if code == "US" || code == "CA" {
		return tr(lang, "region_"+code)
	}
	return tr(lang, "region_auto")
}

func (a *App) handleAdmin(w http.ResponseWriter, r *http.Request) {
	p := adminPage{base: a.adminBase(r, "nav_gifts")}
	lang, now := p.Lang, time.Now()
	a.store.view(func(d *Data) {
		p.SortOrder, p.SortOrders = d.Settings.SortOrder, sortOrders
		for _, it := range adminOrder(d) {
			row := adminRow{
				ID: it.ID, Title: it.Title, Image: it.Image,
				PriceText:  itemPrice(it, lang),
				Site:       siteName(it.URL),
				RegionText: regionText(lang, it.Region),
				StatusText: tr(lang, "status_"+it.Status),
				Status:     it.Status,
				Priority:   it.Priority,
				Quantity:   it.Quantity,
			}
			if live, bought := it.reserved(now); live > 0 {
				row.ReservedText = tf(lang, "adm_reserved", live, it.Quantity)
				if bought > 0 {
					row.ReservedText += tf(lang, "adm_bought", bought)
				}
			}
			if it.Priority > 0 {
				p.HotRows = append(p.HotRows, row)
			} else {
				p.Rows = append(p.Rows, row)
			}
		}
	})
	a.render(w, http.StatusOK, "admin", p)
}

// handleOrder saves the order the admin dragged the rows into. A drag
// always switches the list to manual order. When hot= is sent (the IDs in
// the ★ list), the listed gifts get ★ and the other ones in ids lose it.
func (a *App) handleOrder(w http.ResponseWriter, r *http.Request) {
	ids := strings.Split(r.FormValue("ids"), ",")
	if len(ids) == 0 || len(ids) > 1000 {
		http.Error(w, "bad ids", http.StatusBadRequest)
		return
	}
	_, withHot := r.Form["hot"]
	hot := map[string]bool{}
	for _, id := range strings.Split(r.FormValue("hot"), ",") {
		hot[id] = true
	}
	if err := a.store.update(func(d *Data) error {
		if withHot {
			for _, id := range ids {
				if it := d.item(id); it != nil {
					it.Priority = 0
					if hot[id] {
						it.Priority = 1
					}
				}
			}
		}
		d.reorder(ids)
		d.Settings.SortOrder = ""
		return nil
	}); err != nil {
		log.Printf("order: %v", err)
		http.Error(w, "save failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// adminOrder is the gift list as the admin panel shows it. With a sort on,
// ★ gifts come first, as on the site; reserved gifts stay in place.
func adminOrder(d *Data) []*Item {
	items := sortedItems(d.Items, d.Settings.SortOrder, d.Rates)
	if d.Settings.SortOrder != "" {
		slices.SortStableFunc(items, func(a, b *Item) int { return b.Priority - a.Priority })
	}
	return items
}

// handleSort switches the gift list between the drag order and a sort.
// Going back to the drag order keeps the list as it was last shown, so
// dragging starts from the sorted order rather than the old one.
func (a *App) handleSort(w http.ResponseWriter, r *http.Request) {
	order := r.FormValue("sort")
	if !validSortOrder(order) {
		http.Error(w, "bad sort", http.StatusBadRequest)
		return
	}
	if err := a.store.update(func(d *Data) error {
		if order == "" && d.Settings.SortOrder != "" {
			d.Items = adminOrder(d)
		}
		d.Settings.SortOrder = order
		return nil
	}); err != nil {
		log.Printf("sort: %v", err)
		http.Error(w, "save failed", http.StatusInternalServerError)
		return
	}
	redirectMsg(w, r, "/admin", "saved")
}

// ---- add / edit ----

func parseShopURL(raw string) (*url.URL, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, false
	}
	return u, true
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
		Fetch: fetchInfo(meta, ferr), CreatedAt: now, UpdatedAt: now,
	}
	if it.Region == "" {
		it.Region = detectRegion(meta.FinalURL) // short links such as a.co
	}
	if meta.Price != "" {
		it.PriceCheckedAt = &now
	}
	it.Unavailable = meta.Unavailable
	it.Format = meta.Format
	if it.Title == "" {
		it.Title = siteName(u.String())
	}
	if meta.Image != "" {
		name, err := a.saveImage(ctx, it.ID, meta.Image, u.String())
		if err != nil {
			log.Printf("image %s: %v", u.Host, err)
			it.Fetch = withImageErr(it.Fetch, err)
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
	Item         Item
	Reservations int
}

func (a *App) handleEditForm(w http.ResponseWriter, r *http.Request) {
	p := editPage{base: a.adminBase(r, "gift_page")}
	var found bool
	a.store.view(func(d *Data) {
		if it := d.item(r.PathValue("id")); it != nil {
			found = true
			p.Item = *it
			p.Item.Reservations = nil
			p.Reservations = len(it.Reservations)
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
	if !validStatus[status] {
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
	newImage, imageChanged := "", imageSrc != oldSrc
	var imageNote *FetchInfo
	if imageChanged && imageSrc != "" {
		if _, ok := parseShopURL(imageSrc); !ok {
			imageNote = &FetchInfo{ImageBadURL: true}
			imageChanged = false
		} else {
			ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
			name, err := a.saveImage(ctx, id, imageSrc, u.String())
			cancel()
			if err != nil {
				imageNote = withImageErr(nil, err)
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
		it.FetchNote, it.Fetch = "", imageNote
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
	note := fetchInfo(meta, ferr)
	var newImage string
	if meta.Image != "" {
		name, err := a.saveImage(ctx, id, meta.Image, pageURL)
		if err != nil {
			note = withImageErr(note, err)
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
		if ferr == nil {
			it.Unavailable = meta.Unavailable
		}
		if meta.Format != "" {
			it.Format = meta.Format
		}
		if newImage != "" {
			oldImage = it.Image
			it.Image, it.ImageSrc = newImage, meta.Image
		}
		// Replace the placeholder (the shop's host name) set when nothing could be read.
		if meta.Title != "" && (it.Title == "" || it.Title == siteName(it.URL)) {
			it.Title = meta.Title
		}
		it.FetchNote, it.Fetch = "", note
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
	Settings    Settings
	Addresses   []addressForm
	NotifyTo    string
	MailEnabled bool
	MailFrom    string
	BaseURL     string
}

func (a *App) handleSettingsForm(w http.ResponseWriter, r *http.Request) {
	p := settingsPage{
		base:        a.adminBase(r, "settings_page"),
		NotifyTo:    notifyTo(a.cfg),
		MailEnabled: a.mail.enabled(),
		BaseURL:     a.cfg.BaseURL,
	}
	// ?mailerr= is "off", "bad_to" or the SMTP server's own error text.
	if e := r.URL.Query().Get("mailerr"); e != "" {
		if e == "off" || e == "bad_to" {
			e = tr(p.Lang, "mail_err_"+e)
		}
		p.Error = tf(p.Lang, "mail_err", e)
	}
	if to := r.URL.Query().Get("mailok"); to != "" {
		p.Flash = tf(p.Lang, "mail_test_ok", a.cfg.SMTPHost, to)
	}
	p.MailFrom = a.cfg.SMTPFrom
	a.store.view(func(d *Data) { p.Settings = d.Settings })
	p.Addresses = []addressForm{
		{Prefix: "us", Title: tr(p.Lang, "addr_title_us"), Fields: p.Settings.AddrUS.fields()},
		{Prefix: "ca", Title: tr(p.Lang, "addr_title_ca"), Fields: p.Settings.AddrCA.fields()},
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
		TitleRU:     limit(r.FormValue("title_ru"), 100),
		TitleEN:     limit(r.FormValue("title_en"), 100),
		IntroRU:     limit(r.FormValue("intro_ru"), 2000),
		IntroEN:     limit(r.FormValue("intro_en"), 2000),
		AddrUS:      addressFromForm(r, "us"),
		AddrCA:      addressFromForm(r, "ca"),
		ReserveDays: days,
		NoticeLang:  r.FormValue("notice_lang"),
	}
	if s.NoticeLang != "en" {
		s.NoticeLang = "ru"
	}
	if err := a.store.update(func(d *Data) error {
		s.SortOrder = d.Settings.SortOrder // set on the gift list, not in this form
		d.Settings = s
		return nil
	}); err != nil {
		log.Printf("settings: %v", err)
		http.Error(w, "save failed", http.StatusInternalServerError)
		return
	}
	redirectMsg(w, r, "/admin/settings", "saved")
}

// handleMailTest sends one message to the sender's own address: it proves the
// SMTP login, encryption and sending address in one go.
func (a *App) handleMailTest(w http.ResponseWriter, r *http.Request) {
	if !a.mail.enabled() {
		http.Redirect(w, r, "/admin/settings?mailerr="+"off", http.StatusSeeOther)
		return
	}
	to, err := mail.ParseAddress(strings.TrimSpace(r.FormValue("to")))
	if err != nil {
		http.Redirect(w, r, "/admin/settings?mailerr="+"bad_to", http.StatusSeeOther)
		return
	}
	// The test is the real reservation email, so it meets the same spam filters.
	var days int
	a.store.view(func(d *Data) { days = d.Settings.ReserveDays })
	// The link goes to the list itself: a test has no reservation of its own.
	// It goes out in the admin's language.
	lang := detectLang(r)
	subject, body := a.confirmationMail(lang, a.cfg.BaseURL+"/", tr(lang, "sample_gift"), time.Now().AddDate(0, 0, days))
	err = a.mail.send(to.Address, subject, body)
	if err != nil {
		log.Printf("mail test to %s: %v", to.Address, err)
		http.Redirect(w, r, "/admin/settings?mailerr="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	log.Printf("mail test to %s: accepted by %s", to.Address, a.cfg.SMTPHost)
	http.Redirect(w, r, "/admin/settings?mailok="+url.QueryEscape(to.Address), http.StatusSeeOther)
}
