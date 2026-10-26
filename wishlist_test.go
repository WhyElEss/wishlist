package main

import (
	"bytes"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParseMetaOpenGraph(t *testing.T) {
	doc := `<html><head><title>Fallback</title>
<meta property="og:title" content="Kettle &amp; Co. 1.7 L">
<meta content="/img/kettle.jpg" property='og:image'>
<meta property="product:price:amount" content="49.99">
<meta property="product:price:currency" content="usd">
</head></html>`
	base, _ := url.Parse("https://shop.example/p/1")
	m := parseMeta(doc, base)
	if m.Title != "Kettle & Co. 1.7 L" {
		t.Errorf("title = %q", m.Title)
	}
	if m.Image != "https://shop.example/img/kettle.jpg" {
		t.Errorf("image = %q", m.Image)
	}
	if m.Price != "49.99" || m.Currency != "USD" {
		t.Errorf("price = %q %q", m.Price, m.Currency)
	}
}

func TestParseMetaJSONLD(t *testing.T) {
	doc := `<html><head><title>Shop | Thing</title>
<script type="application/ld+json">{"@context":"https://schema.org","@graph":[
 {"@type":"BreadcrumbList"},
 {"@type":["Product"],"name":"Thing","image":[{"url":"https://cdn.example/t.png"}],
  "offers":{"@type":"AggregateOffer","offers":[{"price":19.5,"priceCurrency":"CAD"}]}}]}
</script></head></html>`
	m := parseMeta(doc, nil)
	if m.Title != "Thing" || m.Image != "https://cdn.example/t.png" || m.Price != "19.5" || m.Currency != "CAD" {
		t.Errorf("got %+v", m)
	}
}

func TestParseMetaTitleFallback(t *testing.T) {
	m := parseMeta("<title>\n  Just a page \n</title>", nil)
	if m.Title != "Just a page" || m.Image != "" || m.Price != "" {
		t.Errorf("got %+v", m)
	}
}

func TestHashPassword(t *testing.T) {
	h, err := hashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(h, "$") {
		t.Errorf("hash contains $: %s", h)
	}
	p, err := parseHash(h)
	if err != nil {
		t.Fatal(err)
	}
	if !p.verify("correct horse") || p.verify("wrong horse") {
		t.Error("verify mismatch")
	}
}

func TestDetectRegion(t *testing.T) {
	for raw, want := range map[string]string{
		"https://www.amazon.com/dp/X": "US",
		"https://amazon.ca/dp/X":      "CA",
		"https://www.ebay.com/itm/1":  "US",
		"https://www.bestbuy.ca/p":    "CA",
		"https://aliexpress.com/item": "",
		"https://notamazon.com/":      "",
	} {
		u, _ := url.Parse(raw)
		if got := detectRegion(u); got != want {
			t.Errorf("%s: got %q, want %q", raw, got, want)
		}
	}
}

// 1x1 transparent PNG.
var pixel, _ = base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNkYAAAAAYAAjCB0C8AAAAASUVORK5CYII=")

func newTestApp(t *testing.T) (*httptest.Server, *App) {
	t.Helper()
	hash, err := hashPassword("admin-password")
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{DataDir: t.TempDir(), AdminHash: hash, TrustProxy: true}
	app, err := newApp(cfg)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(app.routes())
	t.Cleanup(srv.Close)
	return srv, app
}

func newClient(t *testing.T) *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar, Timeout: 30 * time.Second}
}

func body(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}

func TestFlow(t *testing.T) {
	shop := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/pic.png" {
			w.Header().Set("Content-Type", "image/png")
			w.Write(pixel)
			return
		}
		w.Write([]byte(`<html><head><meta property="og:title" content="Test Kettle">
<meta property="og:image" content="/pic.png"><meta property="og:price:amount" content="25">
<meta property="og:price:currency" content="CAD"></head></html>`))
	}))
	defer shop.Close()

	srv, app := newTestApp(t)
	admin := newClient(t)

	// Admin pages need a session.
	resp, err := admin.Get(srv.URL + "/admin")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(resp.Request.URL.Path, "/admin/login") {
		t.Fatalf("unauthenticated /admin ended at %s", resp.Request.URL)
	}
	resp.Body.Close()

	resp, _ = admin.PostForm(srv.URL+"/admin/login", url.Values{"password": {"wrong"}})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong password: %d", resp.StatusCode)
	}
	resp.Body.Close()
	resp, _ = admin.PostForm(srv.URL+"/admin/login", url.Values{"password": {"admin-password"}})
	if resp.Request.URL.Path != "/admin" {
		t.Fatalf("login ended at %s", resp.Request.URL)
	}
	resp.Body.Close()

	// Settings: addresses.
	resp, _ = admin.PostForm(srv.URL+"/admin/settings", url.Values{
		"title_ru": {"Вишлист"}, "title_en": {"Wishlist"},
		"address_us": {"US ADDRESS LINE"}, "address_ca": {"CA ADDRESS LINE"},
		"reserve_days": {"14"},
	})
	resp.Body.Close()

	// Add an item from the fake shop.
	resp, _ = admin.PostForm(srv.URL+"/admin/items", url.Values{"url": {shop.URL + "/kettle"}})
	page := body(t, resp)
	if !strings.Contains(page, "Test Kettle") {
		t.Fatalf("edit page lacks title:\n%s", page)
	}
	var id, image string
	app.store.view(func(d *Data) {
		if len(d.Items) == 1 {
			id, image = d.Items[0].ID, d.Items[0].Image
			if d.Items[0].Price != "25" || d.Items[0].Currency != "CAD" {
				t.Errorf("price %q %q", d.Items[0].Price, d.Items[0].Currency)
			}
		}
	})
	if id == "" || image == "" {
		t.Fatalf("item not stored with image: id=%q image=%q", id, image)
	}
	resp, _ = http.Get(srv.URL + "/img/" + image)
	if b := body(t, resp); !bytes.Equal([]byte(b), pixel) {
		t.Error("served image differs from the shop's")
	}

	// Public list shows it as available.
	guest := newClient(t)
	resp, _ = guest.Get(srv.URL + "/")
	if page := body(t, resp); !strings.Contains(page, "/item/"+id) {
		t.Fatalf("index lacks reserve link:\n%s", page)
	}

	// Two guests reserve at the same time: exactly one wins.
	var wg sync.WaitGroup
	codes := make([]int, 2)
	clients := []*http.Client{guest, newClient(t)}
	for i := range clients {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			resp, err := clients[i].PostForm(srv.URL+"/item/"+id+"/reserve", url.Values{})
			if err != nil {
				t.Error(err)
				return
			}
			codes[i] = resp.StatusCode
			resp.Body.Close()
		}(i)
	}
	wg.Wait()
	ok, conflict := 0, 0
	for _, c := range codes {
		switch c {
		case http.StatusOK:
			ok++
		case http.StatusConflict:
			conflict++
		}
	}
	if ok != 1 || conflict != 1 {
		t.Fatalf("concurrent reserve codes = %v", codes)
	}
	winner := guest
	if codes[0] != http.StatusOK {
		winner = clients[1]
	}

	var token string
	app.store.view(func(d *Data) { token = d.Items[0].Reservations[0].Token })

	// The winner's reservation page shows the Canadian address (CAD shop at
	// a non-regional host, visitor without CF-IPCountry).
	resp, _ = winner.Get(srv.URL + "/r/" + token)
	page = body(t, resp)
	if !strings.Contains(page, "CA ADDRESS LINE") || !strings.Contains(page, `data-region-panel="US" hidden`) {
		t.Errorf("reservation page address selection wrong:\n%s", page)
	}
	// Visitors from the US get the US address preselected.
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/r/"+token, nil)
	req.Header.Set("CF-IPCountry", "US")
	resp, _ = http.DefaultClient.Do(req)
	if page := body(t, resp); !strings.Contains(page, `data-region-panel="CA" hidden`) {
		t.Error("US visitor did not get the US address preselected")
	}

	// The index is greyed for others and shows "yours" to the winner.
	other := newClient(t)
	resp, _ = other.Get(srv.URL + "/")
	if page := body(t, resp); !strings.Contains(page, "card taken") {
		t.Error("index does not grey out the reserved card")
	}
	resp, _ = winner.Get(srv.URL + "/")
	if page := body(t, resp); !strings.Contains(page, "/r/"+token) {
		t.Error("index does not show the winner their reservation")
	}

	// Cross-site posts are refused.
	req, _ = http.NewRequest(http.MethodPost, srv.URL+"/r/"+token+"/cancel", nil)
	req.Header.Set("Origin", "https://evil.example")
	resp, _ = http.DefaultClient.Do(req)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("cross-site cancel: %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Mark as bought, then cancel: the item is free again.
	resp, _ = winner.PostForm(srv.URL+"/r/"+token+"/purchased", url.Values{})
	resp.Body.Close()
	app.store.view(func(d *Data) {
		if d.Items[0].Reservations[0].PurchasedAt == nil {
			t.Error("purchase not recorded")
		}
	})
	resp, _ = winner.PostForm(srv.URL+"/r/"+token+"/cancel", url.Values{})
	resp.Body.Close()
	resp, _ = other.Get(srv.URL + "/")
	if page := body(t, resp); strings.Contains(page, "card taken") {
		t.Error("card still taken after cancel")
	}
	resp, _ = winner.Get(srv.URL + "/r/" + token)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("cancelled reservation page: %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestSweepExpires(t *testing.T) {
	_, app := newTestApp(t)
	now := time.Now()
	app.store.update(func(d *Data) error {
		d.Items = append(d.Items, &Item{ID: "x", Title: "X", Quantity: 1, Status: statusActive,
			Reservations: []*Reservation{
				{ID: "old", Token: "t1", CreatedAt: now.AddDate(0, 0, -20), ExpiresAt: now.Add(-time.Hour)},
			}})
		return nil
	})
	app.sweep(now)
	app.store.view(func(d *Data) {
		if n := len(d.Items[0].Reservations); n != 0 {
			t.Errorf("expired reservation kept: %d", n)
		}
		if !d.Items[0].available(now) {
			t.Error("item not available after expiry")
		}
	})
}
