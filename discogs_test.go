package main

import (
	"context"
	"strings"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestDiscogsRef(t *testing.T) {
	for raw, want := range map[string]string{
		"https://www.discogs.com/master/239695-Various-The-Casablanca-Records-Story":  "master 239695",
		"https://www.discogs.com/release/201415-Various-The-Casablanca-Records-Story": "release 201415",
		"https://www.discogs.com/fr/release/201415":                                   "release 201415",
		"https://www.discogs.com/sell/item/123456":                                    "listing 123456",
		"https://www.discogs.com/shop/item/3170084632":                                "listing 3170084632",
		"https://www.discogs.com/sell/release/21433207":                               "release 21433207",
		"https://www.discogs.com/artist/1-Someone":                                    "",
	} {
		u, _ := url.Parse(raw)
		kind, id, ok := discogsRef(u)
		got := ""
		if ok {
			got = kind + " " + id
		}
		if got != want {
			t.Errorf("%s: got %q, want %q", raw, got, want)
		}
	}
}

func TestDiscogsFetch(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != discogsUA {
			http.Error(w, "no UA", http.StatusForbidden)
			return
		}
		switch r.URL.Path {
		case "/masters/239695":
			w.Write([]byte(`{"title":"The Casablanca Records Story","artists":[{"name":"Various"}],
				"images":[{"type":"secondary","uri":"https://i.example/2.jpg"},{"type":"primary","uri":"https://i.example/1.jpg"}],
				"lowest_price":8.0,"main_release":201415}`))
		case "/releases/201415":
			w.Write([]byte(`{"title":"Love To Love You Baby","artists":[{"name":"Donna Summer (2)"}],
				"images":[{"type":"secondary","uri":"https://i.example/3.jpg"}],"lowest_price":8.0,
				"formats":[{"name":"CD"},{"name":"Box Set"}]}`))
		case "/marketplace/listings/3170084632":
			if r.URL.Query().Get("curr_abbr") != "USD" {
				t.Errorf("listing asked in %q", r.URL.Query().Get("curr_abbr"))
			}
			w.Write([]byte(`{"status":"For Sale","price":{"value":169.99,"currency":"USD"},
				"release":{"artist":"Army Of Lovers","title":"Massive Luxury Overdose (Ultimate Edition)","format":"2xLP, Album, RE, Vio",
				"images":[{"type":"primary","uri":"https://i.example/aol.jpg"}]}}`))
		case "/marketplace/stats/201415":
			if r.URL.Query().Get("curr_abbr") != "USD" {
				t.Errorf("stats asked in %q", r.URL.Query().Get("curr_abbr"))
			}
			w.Write([]byte(`{"num_for_sale":20,"lowest_price":{"value":8.5,"currency":"USD"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer api.Close()

	_, app := newTestApp(t)
	app.discogs.base = api.URL

	m, err := app.fetchMeta(context.Background(), "https://www.discogs.com/master/239695-Various-The-Casablanca-Records-Story")
	if err != nil {
		t.Fatal(err)
	}
	if m.Title != "Various – The Casablanca Records Story" || m.Image != "https://i.example/1.jpg" || m.Price != "8.00" || m.Currency != "USD" || m.Format != formatCD {
		t.Errorf("master: %+v", m)
	}

	m, err = app.fetchMeta(context.Background(), "https://www.discogs.com/release/201415-Donna-Summer")
	if err != nil {
		t.Fatal(err)
	}
	if m.Title != "Donna Summer – Love To Love You Baby" || m.Image != "https://i.example/3.jpg" || m.Price != "8.50" || m.Currency != "USD" {
		t.Errorf("release: %+v", m)
	}

	m, err = app.fetchMeta(context.Background(), "https://www.discogs.com/sell/release/201415")
	if err != nil {
		t.Fatal(err)
	}
	if m.Title != "Donna Summer – Love To Love You Baby" || m.Price != "8.50" || m.Currency != "USD" {
		t.Errorf("sell/release: %+v", m)
	}
	if got := itemPrice(&Item{URL: "https://www.discogs.com/sell/release/201415", Price: "8.50", Currency: "USD"}, "en"); got != "from $8.50" {
		t.Errorf("sell/release price = %q, want a from-price", got)
	}

	m, err = app.fetchMeta(context.Background(), "https://www.discogs.com/shop/item/3170084632")
	if err != nil {
		t.Fatal(err)
	}
	if m.Title != "Army Of Lovers – Massive Luxury Overdose (Ultimate Edition)" || m.Image != "https://i.example/aol.jpg" || m.Price != "169.99" || m.Currency != "USD" || m.Format != formatVinyl {
		t.Errorf("listing: %+v", m)
	}
	if got := itemPrice(&Item{URL: "https://www.discogs.com/shop/item/3170084632", Price: "169.99", Currency: "USD"}, "en"); got != "$169.99" {
		t.Errorf("listing price = %q, want a plain price", got)
	}

	it := &Item{URL: "https://www.discogs.com/master/239695", Price: "8.00", Currency: "USD"}
	if got := itemPrice(it, "en"); got != "from $8.00" {
		t.Errorf("itemPrice en = %q", got)
	}
	if got := itemPrice(it, "ru"); got != "от $8.00" {
		t.Errorf("itemPrice ru = %q", got)
	}
}

func TestDiscogsFormats(t *testing.T) {
	for desc, want := range map[string]string{
		"2xLP, Album, RE, Vio": formatVinyl,
		"7\", Single":          formatVinyl,
		"3x12\", Comp":         formatVinyl,
		"CD, Album":            formatCD,
		"2xCD, Comp":           formatCD,
		"Cass, Album":          formatCassette,
		"File, MP3":            "",
	} {
		if got := formatFromDescription(desc); got != want {
			t.Errorf("formatFromDescription(%q) = %q, want %q", desc, got, want)
		}
	}
	if formatFromNames([]string{"Box Set", "Vinyl"}) != formatVinyl || formatFromNames([]string{"CD", "Box Set"}) != formatCD {
		t.Error("formatFromNames")
	}
	if gradingFor("ru", "").MediaMint != gradings["ru"][formatVinyl].MediaMint {
		t.Error("unknown format should fall back to vinyl")
	}
}

func TestDiscogsConditionBlock(t *testing.T) {
	srv, app := newTestApp(t)
	app.store.update(func(d *Data) error {
		d.Items = []*Item{
			{ID: "dg", URL: "https://www.discogs.com/sell/release/21433207", Title: "Army Of Lovers", Format: formatVinyl, Quantity: 1, Status: statusActive},
			{ID: "az", URL: "https://www.amazon.ca/dp/X", Title: "Kindle", Quantity: 1, Status: statusActive},
		}
		return nil
	})
	get := func(path, lang string) string {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+path, nil)
		req.Header.Set("Accept-Language", lang)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return body(t, resp)
	}
	page := get("/item/dg", "ru")
	for _, want := range []string{"Media Condition", "Sleeve Condition", gradings["ru"][formatVinyl].MediaNM, gradings["ru"][formatVinyl].SleeveNM} {
		if !strings.Contains(page, want) {
			t.Errorf("Discogs confirm step (ru) lacks %q", want)
		}
	}
	if strings.Contains(page, gradings["en"][formatVinyl].MediaNM) {
		t.Error("Russian page shows the English original")
	}
	if page := get("/item/dg", "en"); !strings.Contains(page, gradings["en"][formatVinyl].SleeveNM) {
		t.Error("English page lacks the Discogs original text")
	}
	if page := get("/item/az", "ru"); strings.Contains(page, "Media Condition") {
		t.Error("non-Discogs gift shows the condition block")
	}
	list := get("/", "ru")
	if strings.Count(list, "Только Mint или NM") != 1 {
		t.Error("the list should mark exactly the Discogs card")
	}
}
