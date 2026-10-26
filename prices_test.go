package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNextPriceRefresh(t *testing.T) {
	loc := time.FixedZone("EDT", -4*3600)
	for _, c := range []struct{ now, want string }{
		{"2026-10-26 02:00", "2026-10-26 06:00"},
		{"2026-10-26 06:00", "2026-10-27 06:00"},
		{"2026-10-26 23:59", "2026-10-27 06:00"},
	} {
		now, _ := time.ParseInLocation("2006-01-02 15:04", c.now, loc)
		if got := nextPriceRefresh(now).Format("2006-01-02 15:04"); got != c.want {
			t.Errorf("next after %s = %s, want %s", c.now, got, c.want)
		}
	}
}

func TestRefreshPrices(t *testing.T) {
	shop := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cheaper":
			w.Write([]byte(`<meta property="og:title" content="Kettle"><meta property="product:price:amount" content="19.99"><meta property="product:price:currency" content="CAD">`))
		case "/same":
			w.Write([]byte(`<meta property="og:title" content="Mug"><meta property="product:price:amount" content="5"><meta property="product:price:currency" content="CAD">`))
		case "/blocked":
			http.Error(w, "Access Denied", http.StatusForbidden)
		case "/archived":
			t.Error("archived gift was fetched")
		}
	}))
	defer shop.Close()

	_, app := newTestApp(t)
	app.priceRefreshPause = 0
	app.store.update(func(d *Data) error {
		d.Items = []*Item{
			{ID: "cheaper", URL: shop.URL + "/cheaper", Price: "24.99", Currency: "CAD", Quantity: 1, Status: statusActive},
			{ID: "same", URL: shop.URL + "/same", Price: "5", Currency: "CAD", Quantity: 1, Status: statusActive},
			{ID: "blocked", URL: shop.URL + "/blocked", Price: "129.99", Currency: "CAD", Quantity: 1, Status: statusActive},
			{ID: "archived", URL: shop.URL + "/archived", Price: "1", Quantity: 1, Status: statusReceived},
		}
		return nil
	})
	app.refreshPrices(context.Background())

	app.store.view(func(d *Data) {
		want := map[string]string{"cheaper": "19.99", "same": "5", "blocked": "129.99", "archived": "1"}
		for id, price := range want {
			if got := d.item(id).Price; got != price {
				t.Errorf("%s: price %q, want %q", id, got, price)
			}
		}
		if d.item("cheaper").PriceCheckedAt == nil || d.item("same").PriceCheckedAt == nil {
			t.Error("checked items lack PriceCheckedAt")
		}
		if d.item("blocked").PriceCheckedAt != nil {
			t.Error("blocked item marked as checked")
		}
		if d.PricesCheckedAt.IsZero() {
			t.Error("PricesCheckedAt not set")
		}
	})
}

func TestAmazonUnavailable(t *testing.T) {
	noOffer := `<span id="productTitle"> Technique (2009 Remaster) (Vinyl) </span>
<div id="unqualifiedBuyBox"></div><a id="buybox-see-all-buying-choices">See All Buying Options</a>
<div data-feature-name="sims-simsContainer"><span class="a-price-whole">35</span></div>`
	if m := amazonMeta(noOffer, nil, "CAD"); !m.Unavailable || m.Price != "" {
		t.Errorf("no-offer page: %+v", m)
	}
	gone := `<span id="productTitle">MacBook</span><div id="availability"><span> Currently unavailable. </span></div>`
	if m := amazonMeta(gone, nil, "USD"); !m.Unavailable {
		t.Errorf("currently-unavailable page: %+v", m)
	}
	inStock := `<span id="productTitle">Kindle</span><input id="twister-plus-price-data-price" value="189.99" /><input id="twister-plus-price-data-price-unit" value="$" />`
	if m := amazonMeta(inStock, nil, "CAD"); m.Unavailable || m.Price != "189.99" {
		t.Errorf("in-stock page: %+v", m)
	}
}

func TestPriceCheckTracksAvailability(t *testing.T) {
	_, app := newTestApp(t)
	app.priceRefreshPause = 0
	app.store.update(func(d *Data) error {
		d.Items = []*Item{{ID: "v", URL: "https://www.amazon.ca/dp/B002CW4K8W", Title: "Technique", Price: "50.00", Currency: "CAD", Quantity: 1, Status: statusActive}}
		return nil
	})
	shop := Meta{Unavailable: true}
	app.readShop = func(context.Context, string) (Meta, error) { return shop, nil }

	app.refreshPrices(context.Background())
	app.store.view(func(d *Data) {
		it := d.item("v")
		if !it.Unavailable || it.Price != "50.00" {
			t.Errorf("after unavailable: %+v", it)
		}
		if got := itemPrice(it, "ru"); got != "Временно недоступен в магазине" {
			t.Errorf("guests see %q", got)
		}
	})

	shop = Meta{Price: "45.00", Currency: "CAD"}
	app.refreshPrices(context.Background())
	app.store.view(func(d *Data) {
		it := d.item("v")
		if it.Unavailable || it.Price != "45.00" {
			t.Errorf("after back in stock: %+v", it)
		}
		if got := itemPrice(it, "en"); got != "CA$45.00" {
			t.Errorf("guests see %q", got)
		}
	})
}
