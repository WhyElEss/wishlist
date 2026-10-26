package main

import (
	"bytes"
	"context"
	"embed"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

//go:embed templates static
var assets embed.FS

type App struct {
	cfg          Config
	store        *Store
	adminHash    pwHash
	sessionKey   []byte
	imagesDir    string
	client       *http.Client
	tpl          map[string]*template.Template
	reserveLimit *limiter
	loginLimit   *limiter
	amazon       *amazonUS
	mail         *mailer

	// priceRefreshPause spaces out shop requests in the daily price check.
	priceRefreshPause time.Duration
	// readShop is the shop reader the price check uses (a.fetchMeta; tests
	// replace it).
	readShop func(ctx context.Context, raw string) (Meta, error)

	discogs *discogsAPI
}

func newApp(cfg Config) (*App, error) {
	imagesDir := filepath.Join(cfg.DataDir, "images")
	if err := os.MkdirAll(imagesDir, 0o755); err != nil {
		return nil, err
	}
	store, err := openStore(filepath.Join(cfg.DataDir, "wishlist.json"))
	if err != nil {
		return nil, err
	}
	key, err := loadSessionKey(filepath.Join(cfg.DataDir, "session.key"))
	if err != nil {
		return nil, err
	}
	hash, _ := parseHash(cfg.AdminHash) // validated in loadConfig
	tpl, err := loadTemplates()
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 25 * time.Second}
	a := &App{
		cfg:          cfg,
		store:        store,
		adminHash:    hash,
		sessionKey:   key,
		imagesDir:    imagesDir,
		client:       client,
		tpl:          tpl,
		reserveLimit: newLimiter(5, time.Hour),
		loginLimit:   newLimiter(5, 15*time.Minute),
		mail:         &mailer{host: cfg.SMTPHost, port: cfg.SMTPPort, user: cfg.SMTPUser, pass: cfg.SMTPPass, from: cfg.SMTPFrom, helo: heloName(cfg)},
		amazon:       newAmazonUS("https://www.amazon.com", store.usZip),

		priceRefreshPause: 5 * time.Second,

		discogs: &discogsAPI{base: "https://api.discogs.com", client: client},
	}
	a.readShop = a.fetchMeta
	return a, nil
}

func (a *App) routes() http.Handler {
	mux := http.NewServeMux()

	static, _ := fs.Sub(assets, "static")
	mux.Handle("GET /static/", cacheFor(24*time.Hour, http.StripPrefix("/static/", http.FileServerFS(static))))
	mux.HandleFunc("GET /img/{name}", a.handleImage)
	mux.HandleFunc("GET /robots.txt", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte(robotsTxt))
	})
	// Browsers and Safari's Favorites look for these at the site root.
	for _, name := range []string{"favicon.ico", "apple-touch-icon.png"} {
		mux.Handle("GET /"+name, cacheFor(24*time.Hour, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.ServeFileFS(w, r, static, name)
		})))
	}
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok\n")) })

	mux.HandleFunc("GET /{$}", a.handleIndex)
	mux.HandleFunc("GET /lang/{code}", a.handleLang)
	mux.HandleFunc("GET /item/{id}", a.handleReserveForm)
	mux.HandleFunc("POST /item/{id}/reserve", a.handleReserve)
	mux.HandleFunc("GET /r/{token}", a.handleReservation)
	mux.HandleFunc("POST /r/{token}/purchased", a.handlePurchased)
	mux.HandleFunc("POST /r/{token}/cancel", a.handleCancel)

	mux.HandleFunc("GET /admin/login", a.handleLoginForm)
	mux.HandleFunc("POST /admin/login", a.handleLogin)
	mux.HandleFunc("POST /admin/logout", a.handleLogout)
	mux.HandleFunc("GET /admin", a.requireAdmin(a.handleAdmin))
	mux.HandleFunc("POST /admin/items", a.requireAdmin(a.handleAdd))
	mux.HandleFunc("POST /admin/order", a.requireAdmin(a.handleOrder))
	mux.HandleFunc("GET /admin/items/{id}", a.requireAdmin(a.handleEditForm))
	mux.HandleFunc("POST /admin/items/{id}", a.requireAdmin(a.handleEdit))
	mux.HandleFunc("POST /admin/items/{id}/refresh", a.requireAdmin(a.handleRefresh))
	mux.HandleFunc("POST /admin/items/{id}/release", a.requireAdmin(a.handleRelease))
	mux.HandleFunc("POST /admin/items/{id}/delete", a.requireAdmin(a.handleDelete))
	mux.HandleFunc("GET /admin/settings", a.requireAdmin(a.handleSettingsForm))
	mux.HandleFunc("POST /admin/settings", a.requireAdmin(a.handleSettings))
	mux.HandleFunc("POST /admin/settings/mail-test", a.requireAdmin(a.handleMailTest))

	return a.secure(mux)
}

// robotsTxt keeps search engines out but lets link-preview crawlers read the
// public pages, so shared links get a card. Reservation links and the admin
// stay closed to them too.
const robotsTxt = `User-agent: Twitterbot
User-agent: facebookexternalhit
User-agent: TelegramBot
User-agent: Slackbot
User-agent: Discordbot
User-agent: WhatsApp
User-agent: LinkedInBot
User-agent: Cardyb
Allow: /
Disallow: /admin
Disallow: /r/

User-agent: *
Disallow: /
`

func cacheFor(d time.Duration, h http.Handler) http.Handler {
	maxAge := int(d.Seconds())
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age="+itoa(maxAge))
		h.ServeHTTP(w, r)
	})
}

// secure sets the response headers every page shares and rejects cross-site
// form posts.
func (a *App) secure(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hd := w.Header()
		hd.Set("X-Robots-Tag", "noindex, nofollow")
		hd.Set("X-Content-Type-Options", "nosniff")
		hd.Set("X-Frame-Options", "DENY")
		// same-origin: reservation links carry a secret token and must never
		// reach a shop through the Referer header.
		hd.Set("Referrer-Policy", "same-origin")
		hd.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
		if r.Method == http.MethodPost && !a.sameOrigin(r) {
			http.Error(w, "cross-site request refused", http.StatusForbidden)
			return
		}
		h.ServeHTTP(w, r)
	})
}

func (a *App) sameOrigin(r *http.Request) bool {
	o := r.Header.Get("Origin")
	if o == "" {
		return true
	}
	u, err := url.Parse(o)
	if err != nil {
		return false
	}
	if strings.EqualFold(u.Host, r.Host) {
		return true
	}
	if a.cfg.BaseURL != "" {
		if b, err := url.Parse(a.cfg.BaseURL); err == nil && strings.EqualFold(u.Host, b.Host) {
			return true
		}
	}
	return false
}

func isHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// origin is the scheme and host visitors use, for absolute links shown on pages.
func (a *App) origin(r *http.Request) string {
	if a.cfg.BaseURL != "" {
		return a.cfg.BaseURL
	}
	scheme := "http"
	if isHTTPS(r) {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func loadTemplates() (map[string]*template.Template, error) {
	pages := []string{"index", "reserve", "reservation", "message", "login", "admin", "edit", "settings"}
	m := map[string]*template.Template{}
	for _, p := range pages {
		t, err := template.New(p).Funcs(templateFuncs).ParseFS(assets, "templates/layout.html", "templates/grading.html", "templates/"+p+".html")
		if err != nil {
			return nil, err
		}
		m[p] = t
	}
	return m, nil
}

// base is embedded in every page's data and read by the layout.
type base struct {
	Lang      string
	PageTitle string
	SiteTitle string
	Path      string
	Flash     string
	Error     string
	Version   string
	Admin     bool
	NoNav     bool // login page

	// For link previews (Open Graph), always in English: public origin, the
	// English site title (or the page's own, e.g. a gift's name) and intro.
	Origin      string
	OGTitle     string
	Description string
}

func (a *App) base(r *http.Request, lang, pageTitle string) base {
	var site, siteEN, introEN string
	a.store.view(func(d *Data) {
		site, siteEN, introEN = d.Settings.title(lang), d.Settings.TitleEN, d.Settings.IntroEN
	})
	ogTitle := pageTitle
	if pageTitle == "" {
		pageTitle, ogTitle = site, siteEN
	}
	return base{Lang: lang, PageTitle: pageTitle, SiteTitle: site, Path: r.URL.Path, Version: version,
		Origin: a.origin(r), OGTitle: ogTitle, Description: introEN}
}

func (a *App) render(w http.ResponseWriter, status int, name string, data any) {
	var buf bytes.Buffer
	if err := a.tpl[name].ExecuteTemplate(&buf, "layout", data); err != nil {
		log.Printf("template %s: %v", name, err)
		http.Error(w, "template error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	w.Write(buf.Bytes())
}

type messagePage struct {
	base
	Text string
}

func (a *App) renderMessage(w http.ResponseWriter, r *http.Request, status int, text string) {
	lang := detectLang(r)
	a.render(w, status, "message", messagePage{base: a.base(r, lang, ""), Text: text})
}

// sweeper drops expired reservations and sends reminders before expiry.
func (a *App) sweeper(ctx context.Context) {
	t := time.NewTicker(10 * time.Minute)
	defer t.Stop()
	for {
		a.sweep(time.Now())
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

const reminderBefore = 3 * 24 * time.Hour

type reminder struct {
	email, lang, token, title string
	expires                   time.Time
}

func (a *App) sweep(now time.Time) {
	var expired []string
	var reminders []reminder
	var spoilers bool
	err := a.store.update(func(d *Data) error {
		spoilers = d.Settings.ShowReservations
		changed := false
		for _, it := range d.Items {
			kept := it.Reservations[:0]
			for _, r := range it.Reservations {
				if !r.live(now) {
					expired = append(expired, it.Title)
					changed = true
					continue
				}
				if r.PurchasedAt == nil && r.Email != "" && !r.ReminderSent &&
					r.ExpiresAt.Sub(now) <= reminderBefore && r.ExpiresAt.Sub(r.CreatedAt) > reminderBefore {
					r.ReminderSent = true
					changed = true
					reminders = append(reminders, reminder{r.Email, r.Lang, r.Token, it.Title, r.ExpiresAt})
				}
				kept = append(kept, r)
			}
			it.Reservations = kept
		}
		if !changed {
			return errNoChange
		}
		return nil
	})
	if err != nil {
		log.Printf("sweep: %v", err)
		return
	}
	for _, title := range expired {
		if spoilers {
			a.notify("бронь истекла", "Бронь истекла, подарок снова доступен: "+title)
		} else {
			a.notify("бронь истекла", "Одна бронь истекла, подарок снова доступен.")
		}
	}
	for _, rm := range reminders {
		go a.sendReminder(rm)
	}
}

func (a *App) sendReminder(rm reminder) {
	link := a.cfg.BaseURL + "/r/" + rm.token
	subject := tf(rm.lang, "mail_reminder_subject", rm.title)
	body := tf(rm.lang, "mail_reminder_body", rm.title, a.siteHost(), formatDate(rm.lang, rm.expires), link)
	if err := a.mail.send(rm.email, subject, body); err != nil {
		log.Printf("reminder mail: %v", err)
	}
}

// confirmationMail is the message a guest gets right after reserving; link
// is the reservation page.
func (a *App) confirmationMail(lang, link, title string, expires time.Time) (subject, body string) {
	return tf(lang, "mail_confirm_subject", title),
		tf(lang, "mail_confirm_body", title, a.siteHost(), formatDate(lang, expires), link)
}

// siteHost is the site's public name for mail text (wishlist.stanislavski.me).
func (a *App) siteHost() string {
	if u, err := url.Parse(a.cfg.BaseURL); err == nil && u.Host != "" {
		return u.Host
	}
	return a.cfg.BaseURL
}

func (a *App) sendConfirmation(email, lang, token, title string, expires time.Time) {
	subject, body := a.confirmationMail(lang, a.cfg.BaseURL+"/r/"+token, title, expires)
	if err := a.mail.send(email, subject, body); err != nil {
		log.Printf("confirmation mail: %v", err)
	}
}
