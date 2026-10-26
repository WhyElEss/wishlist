package main

import (
	"bytes"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
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
		"us_name": {"Test Person"}, "us_line1": {"US ADDRESS LINE"}, "us_postal": {"14132"}, "us_country": {"US"},
		"ca_name": {"Test Person"}, "ca_line1": {"CA ADDRESS LINE"}, "ca_city": {"Toronto"},
		"reserve_days": {"14"},
	})
	resp.Body.Close()

	app.store.view(func(d *Data) {
		if d.Settings.AddrUS.PostalCode != "14132" || d.Settings.AddrCA.City != "Toronto" {
			t.Errorf("addresses not saved: %+v %+v", d.Settings.AddrUS, d.Settings.AddrCA)
		}
	})
	if z := app.store.usZip(); z != "14132" {
		t.Errorf("amazon zip = %q", z)
	}

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
	if !strings.Contains(page, `data-copy-value="CA ADDRESS LINE"`) || !strings.Contains(page, `data-region-panel="US" hidden`) {
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

func TestAddressText(t *testing.T) {
	a := Address{Name: "Yurii Stanislavskyi", Line1: "STE E-6420 Inducon Drive W", Line2: "SHP162016",
		City: "Sanborn", Province: "NY", PostalCode: "14132", Country: "US"}
	want := "Yurii Stanislavskyi\nSTE E-6420 Inducon Drive W\nSHP162016\nSanborn, NY 14132\nUS"
	if got := a.text(); got != want {
		t.Errorf("text() = %q, want %q", got, want)
	}
	if (Address{Country: "CA"}).empty() != true || a.empty() {
		t.Error("empty() wrong")
	}
}

func itemIDs(d *Data) string {
	var ids []string
	for _, it := range d.Items {
		ids = append(ids, it.ID)
	}
	return strings.Join(ids, ",")
}

func TestReorder(t *testing.T) {
	d := &Data{Items: []*Item{{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"}}}
	d.reorder([]string{"c", "a", "zzz", "c"})
	if got := itemIDs(d); got != "c,a,b,d" {
		t.Errorf("reorder = %s, want c,a,b,d", got)
	}
}

func TestOldFileIsReversedOnce(t *testing.T) {
	path := t.TempDir() + "/wishlist.json"
	old := `{"settings":{"reserve_days":14},"items":[{"id":"old"},{"id":"mid"},{"id":"new"}]}`
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ { // the second open must not reverse again
		s, err := openStore(path)
		if err != nil {
			t.Fatal(err)
		}
		s.view(func(d *Data) {
			if got := itemIDs(d); got != "new,mid,old" {
				t.Errorf("open %d: order = %s, want new,mid,old", i, got)
			}
		})
	}
}

func TestOrderEndpoint(t *testing.T) {
	srv, app := newTestApp(t)
	app.store.update(func(d *Data) error {
		for _, id := range []string{"a", "b", "c"} {
			d.Items = append(d.Items, &Item{ID: id, Title: id, Quantity: 1, Status: statusActive})
		}
		return nil
	})
	post := func(cookie string) int {
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/admin/order", strings.NewReader("ids=c,a,b"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if cookie != "" {
			req.Header.Set("Cookie", adminCookie+"="+cookie)
		}
		resp, err := (&http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if code := post(""); code != http.StatusSeeOther {
		t.Errorf("without session: %d, want redirect to login", code)
	}
	app.store.view(func(d *Data) {
		if got := itemIDs(d); got != "a,b,c" {
			t.Errorf("order changed without session: %s", got)
		}
	})
	if code := post(app.signSession(time.Now().Add(time.Hour))); code != http.StatusNoContent {
		t.Errorf("with session: %d", code)
	}
	app.store.view(func(d *Data) {
		if got := itemIDs(d); got != "c,a,b" {
			t.Errorf("order = %s, want c,a,b", got)
		}
	})
	resp, _ := http.Get(srv.URL + "/")
	page := body(t, resp)
	if ic, ia := strings.Index(page, "/item/c"), strings.Index(page, "/item/a"); ic < 0 || ia < 0 || ic > ia {
		t.Error("public list does not follow the admin order")
	}

	// ★ lifts a gift above the manual order and marks its card.
	app.store.update(func(d *Data) error {
		d.item("b").Priority = 1
		return nil
	})
	resp, _ = http.Get(srv.URL + "/")
	page = body(t, resp)
	ib, ic, ia := strings.Index(page, "/item/b"), strings.Index(page, "/item/c"), strings.Index(page, "/item/a")
	if !(ib >= 0 && ib < ic && ic < ia) {
		t.Errorf("public order with ★ on b: b=%d c=%d a=%d, want b, c, a", ib, ic, ia)
	}
	if !strings.Contains(page, `class="card hot"`) {
		t.Error("★ card is not marked hot")
	}
}

func TestFixedRegionOffersOneAddress(t *testing.T) {
	srv, app := newTestApp(t)
	app.store.update(func(d *Data) error {
		d.Settings.AddrUS = Address{Name: "Me", Line1: "US WAREHOUSE", PostalCode: "14132", Country: "US"}
		d.Settings.AddrCA = Address{Name: "Me", Line1: "CA HOME", City: "Kitchener", Country: "CA"}
		d.Items = []*Item{
			{ID: "us", URL: "https://www.amazon.com/dp/X", Title: "US gift", Region: "US", Quantity: 1, Status: statusActive},
			{ID: "auto", URL: "https://shop.example/x", Title: "Auto gift", Quantity: 1, Status: statusActive},
		}
		return nil
	})
	reserve := func(id string) string {
		resp, err := newClient(t).PostForm(srv.URL+"/item/"+id+"/reserve", url.Values{})
		if err != nil {
			t.Fatal(err)
		}
		return body(t, resp)
	}

	page := reserve("us")
	if !strings.Contains(page, `data-copy-value="US WAREHOUSE"`) {
		t.Error("US gift: US address missing")
	}
	if strings.Contains(page, "CA HOME") || strings.Contains(page, `data-region-panel="CA"`) || strings.Contains(page, `class="tabs"`) {
		t.Error("US gift: Canadian address or the country switch is offered")
	}

	page = reserve("auto")
	if !strings.Contains(page, "US WAREHOUSE") || !strings.Contains(page, "CA HOME") || !strings.Contains(page, `class="tabs"`) {
		t.Error("auto gift: both addresses and the switch expected")
	}
}

func TestIconsAndLinkPreview(t *testing.T) {
	srv, app := newTestApp(t)
	app.cfg.BaseURL = "https://wishlist.example"
	for path, ctype := range map[string]string{
		"/favicon.ico":           "image/",
		"/apple-touch-icon.png":  "image/png",
		"/static/favicon-32.png": "image/png",
		"/static/og.png":         "image/png",
		"/static/icon.svg":       "image/svg+xml",
	} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), ctype) {
			t.Errorf("%s: %d %q", path, resp.StatusCode, resp.Header.Get("Content-Type"))
		}
	}

	resp, _ := http.Get(srv.URL + "/")
	page := body(t, resp)
	for _, want := range []string{
		`<link rel="apple-touch-icon" sizes="180x180" href="/apple-touch-icon.png">`,
		`<meta property="og:image" content="https://wishlist.example/static/og.png?v=`,
		`<meta name="twitter:card" content="summary_large_image">`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("index lacks %s", want)
		}
	}

	// Previews stay English even for a Russian-language request.
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/", nil)
	req.Header.Set("Accept-Language", "ru-RU,ru;q=0.9")
	resp, _ = http.DefaultClient.Do(req)
	page = body(t, resp)
	if !strings.Contains(page, `<title>Мой вишлист</title>`) || !strings.Contains(page, `<meta property="og:title" content="My wishlist">`) ||
		!strings.Contains(page, `<meta property="og:locale" content="en_US">`) {
		t.Error("Russian request: page should be Russian, preview tags English")
	}

	resp, _ = http.Get(srv.URL + "/robots.txt")
	robots := body(t, resp)
	if !strings.Contains(robots, "User-agent: Twitterbot") || !strings.Contains(robots, "User-agent: *\nDisallow: /\n") {
		t.Errorf("robots.txt:\n%s", robots)
	}
}
