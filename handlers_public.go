package main

import (
	"errors"
	"log"
	"net/http"
	"net/mail"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	mineCookie = "wl_mine"
	maxMine    = 20
)

type cardView struct {
	ID, URL, Title, Image, PriceText, Site, Notes string
	Priority                                      int
	Quantity, Left                                int
	Available, Unavailable                        bool
	MineToken                                     string
}

func makeCard(it *Item, now time.Time, lang string) cardView {
	return cardView{
		ID:        it.ID,
		URL:       it.URL,
		Title:     it.Title,
		Image:     it.Image,
		PriceText: itemPrice(it, lang),
		Site:      siteName(it.URL),
		Notes:     it.Notes,
		Priority:  it.Priority,
		Quantity:  it.Quantity,
		Left:      it.left(now),
		Available: it.available(now),

		Unavailable: it.Unavailable,
	}
}

// mineTokens reads the reservation tokens this browser holds.
func mineTokens(r *http.Request) []string {
	c, err := r.Cookie(mineCookie)
	if err != nil || c.Value == "" {
		return nil
	}
	return strings.Split(c.Value, ".")
}

func (a *App) setMine(w http.ResponseWriter, r *http.Request, tokens []string) {
	if len(tokens) > maxMine {
		tokens = tokens[len(tokens)-maxMine:]
	}
	c := &http.Cookie{
		Name:     mineCookie,
		Value:    strings.Join(tokens, "."),
		Path:     "/",
		MaxAge:   365 * 24 * 3600,
		HttpOnly: true,
		Secure:   isHTTPS(r),
		SameSite: http.SameSiteLaxMode,
	}
	if len(tokens) == 0 {
		c.MaxAge = -1
	}
	http.SetCookie(w, c)
}

func (a *App) addMine(w http.ResponseWriter, r *http.Request, token string) {
	tokens := mineTokens(r)
	for _, t := range tokens {
		if t == token {
			return
		}
	}
	a.setMine(w, r, append(tokens, token))
}

func (a *App) dropMine(w http.ResponseWriter, r *http.Request, token string) {
	var kept []string
	for _, t := range mineTokens(r) {
		if t != token {
			kept = append(kept, t)
		}
	}
	a.setMine(w, r, kept)
}

type indexPage struct {
	base
	Intro string
	Cards []cardView
}

func (a *App) handleIndex(w http.ResponseWriter, r *http.Request) {
	lang := detectLang(r)
	mine := map[string]bool{}
	for _, t := range mineTokens(r) {
		mine[t] = true
	}
	now := time.Now()
	var cards []cardView
	var intro string
	a.store.view(func(d *Data) {
		intro = d.Settings.intro(lang)
		for _, it := range d.Items {
			if it.Status != statusActive {
				continue
			}
			c := makeCard(it, now, lang)
			for _, res := range it.Reservations {
				if res.live(now) && mine[res.Token] {
					c.MineToken = res.Token
				}
			}
			cards = append(cards, c)
		}
	})
	// Gifts that can still be taken first; among them "really want" (★) above
	// the rest; within each group the admin's own order.
	sort.SliceStable(cards, func(i, j int) bool {
		if cards[i].Available != cards[j].Available {
			return cards[i].Available
		}
		return cards[i].Priority > cards[j].Priority
	})
	p := indexPage{base: a.base(r, lang, ""), Intro: intro, Cards: cards}
	if r.URL.Query().Get("m") == "cancelled" {
		p.Flash = tr(lang, "cancelled")
	}
	a.render(w, http.StatusOK, "index", p)
}

func (a *App) handleLang(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	if code != "ru" && code != "en" {
		http.NotFound(w, r)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "lang", Value: code, Path: "/", MaxAge: 365 * 24 * 3600, SameSite: http.SameSiteLaxMode, Secure: isHTTPS(r)})
	next := r.URL.Query().Get("next")
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.Contains(next, `\`) {
		next = "/"
	}
	http.Redirect(w, r, next, http.StatusSeeOther)
}

var reImageName = regexp.MustCompile(`^[A-Za-z0-9_-]+\.(jpg|png|webp|gif|avif)$`)

func (a *App) handleImage(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !reImageName.MatchString(name) {
		http.NotFound(w, r)
		return
	}
	// File names are unique per download, so they can be cached for good.
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.ServeFile(w, r, filepath.Join(a.imagesDir, name))
}

type reservePage struct {
	base
	Card         cardView
	Days         int
	EmailEnabled bool
}

func (a *App) handleReserveForm(w http.ResponseWriter, r *http.Request) {
	lang := detectLang(r)
	now := time.Now()
	var card cardView
	var found bool
	var days int
	a.store.view(func(d *Data) {
		days = d.Settings.ReserveDays
		if it := d.item(r.PathValue("id")); it != nil && it.Status == statusActive {
			card, found = makeCard(it, now, lang), true
		}
	})
	if !found {
		a.renderMessage(w, r, http.StatusNotFound, tr(lang, "item_not_found"))
		return
	}
	if !card.Available {
		a.renderMessage(w, r, http.StatusConflict, tr(lang, "unavailable"))
		return
	}
	a.render(w, http.StatusOK, "reserve", reservePage{
		base:         a.base(r, lang, card.Title),
		Card:         card,
		Days:         days,
		EmailEnabled: a.mail.enabled(),
	})
}

func (a *App) handleReserve(w http.ResponseWriter, r *http.Request) {
	lang := detectLang(r)
	if !a.reserveLimit.allow(a.clientIP(r)) {
		a.renderMessage(w, r, http.StatusTooManyRequests, tr(lang, "rate_limited"))
		return
	}
	var email string
	if a.mail.enabled() {
		if e := strings.TrimSpace(r.FormValue("email")); e != "" {
			addr, err := mail.ParseAddress(e)
			if err != nil || len(addr.Address) > 254 {
				a.renderMessage(w, r, http.StatusBadRequest, tr(lang, "bad_email"))
				return
			}
			email = addr.Address
		}
	}

	now := time.Now()
	token := newToken()
	var title string
	var spoilers bool
	var expires time.Time
	err := a.store.update(func(d *Data) error {
		it := d.item(r.PathValue("id"))
		if it == nil || it.Status != statusActive {
			return errNotFound
		}
		if !it.available(now) {
			return errUnavailable
		}
		expires = now.AddDate(0, 0, d.Settings.ReserveDays)
		it.Reservations = append(it.Reservations, &Reservation{
			ID: newID(), Token: token, Email: email, Lang: lang,
			CreatedAt: now, ExpiresAt: expires,
		})
		title, spoilers = it.Title, d.Settings.ShowReservations
		return nil
	})
	switch {
	case errors.Is(err, errNotFound):
		a.renderMessage(w, r, http.StatusNotFound, tr(lang, "item_not_found"))
		return
	case errors.Is(err, errUnavailable):
		a.renderMessage(w, r, http.StatusConflict, tr(lang, "unavailable"))
		return
	case err != nil:
		log.Printf("reserve: %v", err)
		a.renderMessage(w, r, http.StatusInternalServerError, tr(lang, "server_error"))
		return
	}

	a.addMine(w, r, token)
	if spoilers {
		a.notify("подарок забронирован", "Забронирован подарок: "+title)
	} else {
		a.notify("подарок забронирован", "Кто-то забронировал подарок из вашего списка. Какой именно, не показывается: включён режим без спойлеров.")
	}
	if email != "" {
		go a.sendConfirmation(email, lang, token, title, expires)
	}
	http.Redirect(w, r, "/r/"+token, http.StatusSeeOther)
}

type regionView struct {
	Code    string
	Fields  []addrField // filled fields only
	CopyAll string
	Active  bool
}

type reservationPage struct {
	base
	Card      cardView
	Token     string
	SelfURL   string
	ExpiresAt time.Time
	Purchased bool
	Emailed   bool
	Regions   []regionView

	// FixedRegion: the item allows a single address, so there is no switch.
	FixedRegion bool
}

func (a *App) handleReservation(w http.ResponseWriter, r *http.Request) {
	lang := detectLang(r)
	token := r.PathValue("token")
	now := time.Now()
	var p reservationPage
	var found bool
	var itemRegion string
	var settings Settings
	a.store.view(func(d *Data) {
		it, res := d.findReservation(token)
		if it == nil || !res.live(now) {
			return
		}
		found = true
		settings = d.Settings
		p.Card = makeCard(it, now, lang)
		p.ExpiresAt = res.ExpiresAt
		p.Purchased = res.PurchasedAt != nil
		p.Emailed = res.Email != ""
		itemRegion = it.Region
	})
	if !found {
		a.renderMessage(w, r, http.StatusNotFound, tr(lang, "not_found"))
		return
	}
	// A region set on the item is the only address offered: shipping across
	// the border means duties and international postage. "Auto" offers both,
	// starting from the visitor's country.
	codes := []string{"US", "CA"}
	if itemRegion != "" {
		codes = []string{itemRegion}
		p.FixedRegion = true
	} else {
		itemRegion = a.visitorRegion(r)
	}
	for _, code := range codes {
		addr := settings.address(code)
		rv := regionView{Code: code, Active: code == itemRegion}
		if !addr.empty() {
			for _, f := range addr.fields() {
				if f.Value != "" {
					rv.Fields = append(rv.Fields, f)
				}
			}
			rv.CopyAll = addr.text()
		}
		p.Regions = append(p.Regions, rv)
	}
	p.base = a.base(r, lang, tr(lang, "res_title"))
	p.Token = token
	p.SelfURL = a.origin(r) + "/r/" + token
	if r.URL.Query().Get("m") == "purchased" {
		p.Flash = tr(lang, "purchased_done")
	}
	a.addMine(w, r, token)
	a.render(w, http.StatusOK, "reservation", p)
}

func (a *App) handlePurchased(w http.ResponseWriter, r *http.Request) {
	lang := detectLang(r)
	token := r.PathValue("token")
	now := time.Now()
	var title string
	var spoilers, first bool
	err := a.store.update(func(d *Data) error {
		it, res := d.findReservation(token)
		if it == nil || !res.live(now) {
			return errNotFound
		}
		if res.PurchasedAt != nil {
			return errNoChange
		}
		res.PurchasedAt = &now
		res.Email = ""
		title, spoilers, first = it.Title, d.Settings.ShowReservations, true
		return nil
	})
	if errors.Is(err, errNotFound) {
		a.renderMessage(w, r, http.StatusNotFound, tr(lang, "not_found"))
		return
	}
	if err != nil {
		log.Printf("purchased: %v", err)
		a.renderMessage(w, r, http.StatusInternalServerError, tr(lang, "server_error"))
		return
	}
	if first {
		if spoilers {
			a.notify("подарок куплен", "Подарок куплен: "+title)
		} else {
			a.notify("подарок куплен", "Один из подарков отмечен купленным.")
		}
	}
	http.Redirect(w, r, "/r/"+token+"?m=purchased", http.StatusSeeOther)
}

func (a *App) handleCancel(w http.ResponseWriter, r *http.Request) {
	lang := detectLang(r)
	token := r.PathValue("token")
	var title string
	var spoilers bool
	err := a.store.update(func(d *Data) error {
		it, res := d.findReservation(token)
		if it == nil {
			return errNotFound
		}
		kept := it.Reservations[:0]
		for _, x := range it.Reservations {
			if x != res {
				kept = append(kept, x)
			}
		}
		it.Reservations = kept
		title, spoilers = it.Title, d.Settings.ShowReservations
		return nil
	})
	if errors.Is(err, errNotFound) {
		a.dropMine(w, r, token)
		a.renderMessage(w, r, http.StatusNotFound, tr(lang, "not_found"))
		return
	}
	if err != nil {
		log.Printf("cancel: %v", err)
		a.renderMessage(w, r, http.StatusInternalServerError, tr(lang, "server_error"))
		return
	}
	a.dropMine(w, r, token)
	if spoilers {
		a.notify("бронь отменена", "Бронь отменена: "+title)
	} else {
		a.notify("бронь отменена", "Одну бронь отменили, подарок снова доступен.")
	}
	http.Redirect(w, r, "/?m=cancelled", http.StatusSeeOther)
}
