package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"
)

func titles(items []*Item) string {
	var out []string
	for _, it := range items {
		out = append(out, it.Title)
	}
	return strings.Join(out, ", ")
}

func TestSortedItems(t *testing.T) {
	day := func(n int) time.Time { return time.Date(2026, 10, n, 12, 0, 0, 0, time.UTC) }
	byTitle := []*Item{
		{Title: "Yello – Baby"}, {Title: "Александр Зацепин"}, {Title: "Elán – Kamikadze Lover"},
		{Title: "Ёлка"}, {Title: "abba"}, {Title: "Bee Gees"}, {Title: "Ema Sugimoto – アイ"},
	}
	for order, want := range map[string]string{
		"title_asc":  "abba, Bee Gees, Elán – Kamikadze Lover, Ema Sugimoto – アイ, Yello – Baby, Александр Зацепин, Ёлка",
		"title_desc": "Yello – Baby, Ema Sugimoto – アイ, Elán – Kamikadze Lover, Bee Gees, abba, Ёлка, Александр Зацепин",
		"":           "Yello – Baby, Александр Зацепин, Elán – Kamikadze Lover, Ёлка, abba, Bee Gees, Ema Sugimoto – アイ",
	} {
		if got := titles(sortedItems(byTitle, order, nil)); got != want {
			t.Errorf("%q:\n got %s\nwant %s", order, got, want)
		}
	}

	rates := map[string]float64{"USD": 1.4}
	byPrice := []*Item{
		{Title: "no rate", Price: "10", Currency: "GBP"},
		{Title: "cad 100", Price: "100.00", Currency: "CAD"},
		{Title: "gone", Price: "1", Currency: "CAD", Unavailable: true},
		{Title: "usd 80", Price: "80", Currency: "USD"}, // 112 CAD
		{Title: "none"},
		{Title: "cad 1,200", Price: "1,200.00", Currency: "CAD"},
	}
	for order, want := range map[string]string{
		"price_asc":  "cad 100, usd 80, cad 1,200, no rate, gone, none",
		"price_desc": "cad 1,200, usd 80, cad 100, no rate, gone, none",
	} {
		if got := titles(sortedItems(byPrice, order, rates)); got != want {
			t.Errorf("%q:\n got %s\nwant %s", order, got, want)
		}
	}

	byDate := []*Item{{Title: "b", CreatedAt: day(2)}, {Title: "c", CreatedAt: day(3)}, {Title: "a", CreatedAt: day(1)}}
	if got := titles(sortedItems(byDate, "date_desc", nil)); got != "c, b, a" {
		t.Errorf("date_desc: %s", got)
	}
	if got := titles(sortedItems(byDate, "date_asc", nil)); got != "a, b, c" {
		t.Errorf("date_asc: %s", got)
	}
	if titles(byDate) != "b, c, a" {
		t.Error("sortedItems changed the stored order")
	}
}

func TestSortOnSiteAndAdmin(t *testing.T) {
	srv, app := newTestApp(t)
	now := time.Now()
	app.store.update(func(d *Data) error {
		d.Items = []*Item{
			{ID: "c", URL: "https://shop.example/c", Title: "Charlie", Quantity: 1, Status: statusActive},
			{ID: "r", URL: "https://shop.example/r", Title: "Alpha reserved", Quantity: 1, Status: statusActive,
				Reservations: []*Reservation{{ID: "x", Token: "t", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}}},
			{ID: "s", URL: "https://shop.example/s", Title: "Zulu star", Quantity: 1, Status: statusActive, Priority: 1},
			{ID: "b", URL: "https://shop.example/b", Title: "Bravo", Quantity: 1, Status: statusActive},
			{ID: "y", URL: "https://shop.example/y", Title: "Yankee star", Quantity: 1, Status: statusActive, Priority: 1},
		}
		return nil
	})
	admin := newClient(t)
	admin.PostForm(srv.URL+"/admin/login", url.Values{"password": {"admin-password"}})

	resp, _ := admin.PostForm(srv.URL+"/admin/sort", url.Values{"sort": {"bogus"}})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("bad sort order accepted: %d", resp.StatusCode)
	}
	resp.Body.Close()
	admin.PostForm(srv.URL+"/admin/sort", url.Values{"sort": {"title_asc"}})

	// order lists the first letters of the gift titles as the page shows them
	// (site cards: <h2>, admin rows: links to the edit page).
	order := func(page string) string {
		re := regexp.MustCompile(`(?:<h2>(?:<a [^>]*>)?|<a href="/admin/items/[a-z]">)([A-Z])`)
		var out []string
		for _, m := range re.FindAllStringSubmatch(page, -1) {
			out = append(out, strings.ToLower(m[1]))
		}
		return strings.Join(out, "")
	}
	// Site: ★ first (sorted), then the rest (sorted), reserved last.
	resp, _ = http.Get(srv.URL + "/")
	if got := order(body(t, resp)); got != "yzbca" {
		t.Errorf("site order %q, want yzbca", got)
	}
	// Admin: ★ first, reserved not moved; no drag handles.
	resp, _ = admin.Get(srv.URL + "/admin")
	page := body(t, resp)
	if got := order(page); got != "yzabc" {
		t.Errorf("admin order %q, want yzabc", got)
	}
	if strings.Contains(page, "data-sortable") || strings.Contains(page, `class="drag"`) {
		t.Error("drag ordering still offered while a sort is on")
	}
	if !strings.Contains(page, `<option value="title_asc" selected>`) {
		t.Error("the chosen sort is not selected")
	}

	// Saving the settings form keeps the sort.
	admin.PostForm(srv.URL+"/admin/settings", url.Values{"reserve_days": {"14"}})
	var got string
	app.store.view(func(d *Data) { got = d.Settings.SortOrder })
	if got != "title_asc" {
		t.Errorf("settings form reset the sort to %q", got)
	}

	// Back to manual: the list stays as it was last shown, now draggable.
	admin.PostForm(srv.URL+"/admin/sort", url.Values{"sort": {""}})
	resp, _ = admin.Get(srv.URL + "/admin")
	page = body(t, resp)
	if got := order(page); got != "yzabc" || !strings.Contains(page, "data-sortable") {
		t.Errorf("manual admin order %q (want yzabc, the last sorted order) or no drag handles", got)
	}
	// Choosing manual again changes nothing.
	admin.PostForm(srv.URL+"/admin/sort", url.Values{"sort": {""}})
	resp, _ = admin.Get(srv.URL + "/admin")
	if got := order(body(t, resp)); got != "yzabc" {
		t.Errorf("manual → manual reordered the list: %q", got)
	}
}

func TestReadRate(t *testing.T) {
	// The shape measured from the Bank of Canada Valet API on 2026-10-28.
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/observations/FXUSDCAD/json":
			w.Write([]byte(`{"terms":{},"seriesDetail":{},"observations":[{"d":"2026-10-27","FXUSDCAD":{"v":"1.4038"}}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer api.Close()
	_, app := newTestApp(t)
	app.ratesBase = api.URL
	app.store.update(func(d *Data) error {
		d.Items = []*Item{{ID: "u", Price: "10", Currency: "USD"}, {ID: "x", Price: "10", Currency: "XYZ"}, {ID: "c", Price: "10", Currency: "CAD"}}
		return nil
	})
	app.refreshRates(context.Background())
	var rates map[string]float64
	var date string
	app.store.view(func(d *Data) { rates, date = d.Rates, d.RatesDate })
	if rates["USD"] != 1.4038 || date != "2026-10-27" {
		t.Errorf("rates %v on %q", rates, date)
	}
	if _, ok := rates["XYZ"]; ok {
		t.Error("a currency the API does not know got a rate")
	}
}
