package main

import (
	"context"
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
				"lowest_price":8.0}`))
		case "/releases/201415":
			w.Write([]byte(`{"title":"Love To Love You Baby","artists":[{"name":"Donna Summer (2)"}],
				"images":[{"type":"secondary","uri":"https://i.example/3.jpg"}],"lowest_price":8.0}`))
		case "/marketplace/listings/3170084632":
			if r.URL.Query().Get("curr_abbr") != "USD" {
				t.Errorf("listing asked in %q", r.URL.Query().Get("curr_abbr"))
			}
			w.Write([]byte(`{"status":"For Sale","price":{"value":169.99,"currency":"USD"},
				"release":{"artist":"Army Of Lovers","title":"Massive Luxury Overdose (Ultimate Edition)",
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
	if m.Title != "Various – The Casablanca Records Story" || m.Image != "https://i.example/1.jpg" || m.Price != "8.00" || m.Currency != "USD" {
		t.Errorf("master: %+v", m)
	}

	m, err = app.fetchMeta(context.Background(), "https://www.discogs.com/release/201415-Donna-Summer")
	if err != nil {
		t.Fatal(err)
	}
	if m.Title != "Donna Summer – Love To Love You Baby" || m.Image != "https://i.example/3.jpg" || m.Price != "8.50" || m.Currency != "USD" {
		t.Errorf("release: %+v", m)
	}

	m, err = app.fetchMeta(context.Background(), "https://www.discogs.com/shop/item/3170084632")
	if err != nil {
		t.Fatal(err)
	}
	if m.Title != "Army Of Lovers – Massive Luxury Overdose (Ultimate Edition)" || m.Image != "https://i.example/aol.jpg" || m.Price != "169.99" || m.Currency != "USD" {
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
