package main

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// Private gifts are on the list only for the signed-in owner, framed green
// and linked to the shop; guests never see them and cannot reserve them.
func TestPrivateGifts(t *testing.T) {
	srv, app := newTestApp(t)
	app.store.update(func(d *Data) error {
		d.Items = []*Item{
			{ID: "pub", URL: "https://shop.example/pub", Title: "Public gift", Quantity: 1, Status: statusActive},
			{ID: "sec", URL: "https://shop.example/sec", Title: "Secret gift", Quantity: 1, Status: statusPrivate},
		}
		return nil
	})

	guest := getLang(t, newClient(t), srv.URL+"/", "en")
	if !strings.Contains(guest, "Public gift") || strings.Contains(guest, "Secret gift") {
		t.Error("guests must see the active gift and not the private one")
	}
	resp, err := newClient(t).Get(srv.URL + "/item/sec")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("reserve step of a private gift: HTTP %d, want 404", resp.StatusCode)
	}

	admin := newClient(t)
	admin.PostForm(srv.URL+"/admin/login", url.Values{"password": {"admin-password"}})
	owner := getLang(t, admin, srv.URL+"/", "en")
	if !strings.Contains(owner, "Secret gift") {
		t.Fatal("the owner must see the private gift")
	}
	if !strings.Contains(owner, `class="card private linked"`) ||
		!strings.Contains(owner, `href="https://shop.example/sec" target="_blank" rel="noopener noreferrer"`) {
		t.Error("private card must be framed and link to the shop")
	}

	css, err := assets.ReadFile("static/style.css")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(css), ".card.private { border-color: var(--private); box-shadow: 0 0 0 1px var(--private); }") {
		t.Error("style.css lost the green frame for private cards")
	}
}

func TestStatusMenu(t *testing.T) {
	srv, app := newTestApp(t)
	app.store.update(func(d *Data) error {
		d.Items = []*Item{{ID: "g", URL: "https://shop.example/g", Title: "Gift", Quantity: 1, Status: statusActive,
			Reservations: []*Reservation{{Token: "t", Email: "guest@example.com"}}}}
		return nil
	})
	admin := newClient(t)
	admin.PostForm(srv.URL+"/admin/login", url.Values{"password": {"admin-password"}})

	page := getLang(t, admin, srv.URL+"/admin", "en")
	if !strings.Contains(page, `action="/admin/items/g/status"`) || !strings.Contains(page, `<option value="private">Private</option>`) {
		t.Error("admin list has no status menu with Private")
	}

	for _, c := range []struct{ status, want string }{{"private", statusPrivate}, {"bogus", statusPrivate}, {"owned", statusOwned}} {
		status, want := c.status, c.want
		admin.PostForm(srv.URL+"/admin/items/g/status", url.Values{"status": {status}})
		var got, email string
		app.store.view(func(d *Data) { got, email = d.item("g").Status, d.item("g").Reservations[0].Email })
		if got != want {
			t.Errorf("status %q saved as %q, want %q", status, got, want)
		}
		if email != "" {
			t.Errorf("status %q kept the guest's email", status)
		}
	}
}

func TestPriceCheckIncludesPrivate(t *testing.T) {
	_, app := newTestApp(t)
	app.priceRefreshPause = 0
	app.store.update(func(d *Data) error {
		d.Items = []*Item{
			{ID: "p", URL: "https://shop.example/p", Title: "P", Quantity: 1, Status: statusPrivate},
			{ID: "o", URL: "https://shop.example/o", Title: "O", Quantity: 1, Status: statusOwned},
		}
		return nil
	})
	app.readShop = func(context.Context, string) (Meta, error) { return Meta{Price: "10", Currency: "CAD"}, nil }
	app.refreshPrices(context.Background())
	app.store.view(func(d *Data) {
		if d.item("p").Price != "10" {
			t.Error("private gift not checked")
		}
		if d.item("o").Price != "" {
			t.Error("owned gift checked")
		}
	})
}

// Private gifts have their own admin list between the rest and the got list;
// dragging into it makes a gift private, out of it active again. On the site
// the owner sees them after the reserved gifts and before the got ones.
func TestPrivateList(t *testing.T) {
	srv, app := newTestApp(t)
	app.store.update(func(d *Data) error {
		d.Items = []*Item{
			{ID: "p", URL: "https://shop.example/p", Title: "Private gift", Quantity: 1, Status: statusPrivate},
			{ID: "o", URL: "https://shop.example/o", Title: "Owned gift", Quantity: 1, Status: statusOwned},
			{ID: "t", URL: "https://shop.example/t", Title: "Taken gift", Quantity: 1, Status: statusActive,
				Reservations: []*Reservation{{ID: "x", Token: "tok", CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}}},
			{ID: "a", URL: "https://shop.example/a", Title: "Active gift", Quantity: 1, Status: statusActive},
		}
		return nil
	})
	admin := newClient(t)
	admin.PostForm(srv.URL+"/admin/login", url.Values{"password": {"admin-password"}})

	page := getLang(t, admin, srv.URL+"/admin", "en")
	rest, priv, got := strings.Index(page, `data-group="rest"`), strings.Index(page, `data-group="private"`), strings.Index(page, `data-group="got"`)
	if !(rest > 0 && rest < priv && priv < got) {
		t.Fatalf("lists out of order: rest %d, private %d, got %d", rest, priv, got)
	}
	if p := page[priv:got]; !strings.Contains(p, `data-id="p"`) || strings.Contains(p, `data-id="a"`) || strings.Contains(p, `data-id="o"`) {
		t.Error("the private list must hold exactly the private gift")
	}

	owner := getLang(t, admin, srv.URL+"/", "en")
	order := []int{strings.Index(owner, "Active gift"), strings.Index(owner, "Taken gift"), strings.Index(owner, "Private gift"), strings.Index(owner, "Owned gift")}
	for i := 1; i < len(order); i++ {
		if order[i-1] < 0 || order[i-1] > order[i] {
			t.Fatalf("site order for the owner %v, want active, reserved, private, got", order)
		}
	}

	// a dragged into the private list, p out of it; o stays in the got list.
	resp, err := admin.PostForm(srv.URL+"/admin/order", url.Values{"ids": {"p,t,a,o"}, "hot": {""}, "got": {"o"}, "private": {"a"}})
	if err != nil || resp.StatusCode != http.StatusNoContent {
		t.Fatalf("order: %v %v", err, resp)
	}
	want := map[string]string{"a": statusPrivate, "p": statusActive, "o": statusOwned, "t": statusActive}
	app.store.view(func(d *Data) {
		for id, status := range want {
			if s := d.item(id).Status; s != status {
				t.Errorf("%s: status %q, want %q", id, s, status)
			}
		}
	})

	// Private to got in one drag: the got list wins.
	admin.PostForm(srv.URL+"/admin/order", url.Values{"ids": {"p,t,a,o"}, "hot": {""}, "got": {"a,o"}, "private": {""}})
	app.store.view(func(d *Data) {
		if s := d.item("a").Status; s != statusOwned {
			t.Errorf("private dragged to got: %q, want owned", s)
		}
	})
}
