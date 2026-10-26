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
