package main

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// Gifts already received or owned sit in their own list at the bottom of the
// admin; dragging into that list marks a gift "already have", dragging out of
// it makes it active again.
func TestGotListInAdmin(t *testing.T) {
	srv, app := newTestApp(t)
	app.store.update(func(d *Data) error {
		d.Items = []*Item{
			{ID: "a", URL: "https://shop.example/a", Title: "Active gift", Quantity: 1, Status: statusActive},
			{ID: "o", URL: "https://shop.example/o", Title: "Owned gift", Quantity: 1, Status: statusOwned, Priority: 1},
			{ID: "r", URL: "https://shop.example/r", Title: "Received gift", Quantity: 1, Status: statusReceived},
		}
		return nil
	})
	admin := newClient(t)
	admin.PostForm(srv.URL+"/admin/login", url.Values{"password": {"admin-password"}})

	page := getLang(t, admin, srv.URL+"/admin", "en")
	gotList := page[strings.Index(page, `<tbody data-group="got">`):]
	if !strings.Contains(gotList, "Already have or received") || !strings.Contains(gotList, `data-id="o"`) || !strings.Contains(gotList, `data-id="r"`) || strings.Contains(gotList, `data-id="a"`) {
		t.Error("the bottom list must hold exactly the owned and received gifts, even with ★")
	}

	// a dragged into the bottom list, r dragged out into the middle one, o stays.
	resp, err := admin.PostForm(srv.URL+"/admin/order", url.Values{"ids": {"r,a,o"}, "hot": {""}, "got": {"a,o"}})
	if err != nil || resp.StatusCode != http.StatusNoContent {
		t.Fatalf("order: %v %v", err, resp)
	}
	want := map[string]string{"a": statusOwned, "o": statusOwned, "r": statusActive}
	app.store.view(func(d *Data) {
		for id, status := range want {
			if s := d.item(id).Status; s != status {
				t.Errorf("%s: status %q, want %q", id, s, status)
			}
		}
	})

	// An order without got= (older pages) leaves statuses alone.
	admin.PostForm(srv.URL+"/admin/order", url.Values{"ids": {"a,o,r"}, "hot": {""}})
	app.store.view(func(d *Data) {
		if s := d.item("a").Status; s != statusOwned {
			t.Errorf("order without got changed a to %q", s)
		}
	})
}

// On the site only the signed-in owner sees received and owned gifts: grey,
// at the very end, not a link.
func TestGotOnSite(t *testing.T) {
	srv, app := newTestApp(t)
	app.store.update(func(d *Data) error {
		d.Items = []*Item{
			{ID: "o", URL: "https://shop.example/o", Title: "Owned gift", Quantity: 1, Status: statusOwned, Priority: 1},
			{ID: "r", URL: "https://shop.example/r", Title: "Received gift", Quantity: 1, Status: statusReceived},
			{ID: "a", URL: "https://shop.example/a", Title: "Active gift", Quantity: 1, Status: statusActive},
		}
		return nil
	})

	guest := getLang(t, newClient(t), srv.URL+"/", "en")
	if strings.Contains(guest, "Owned gift") || strings.Contains(guest, "Received gift") {
		t.Error("guests must not see received or owned gifts")
	}

	admin := newClient(t)
	admin.PostForm(srv.URL+"/admin/login", url.Values{"password": {"admin-password"}})
	owner := getLang(t, admin, srv.URL+"/", "en")
	a, o, r := strings.Index(owner, "Active gift"), strings.Index(owner, "Owned gift"), strings.Index(owner, "Received gift")
	if a < 0 || o < 0 || r < 0 || !(a < o && a < r) {
		t.Fatalf("the owner must see the got gifts after the active one (%d, %d, %d)", a, o, r)
	}
	if strings.Count(owner, `class="card got"`) != 2 || strings.Count(owner, "Already got it") != 2 {
		t.Error("got cards must be grey, unlinked and labelled")
	}
	if strings.Contains(owner, `href="/item/o"`) || strings.Contains(owner, `href="https://shop.example/o"`) {
		t.Error("a got card must not be a link")
	}
	resp, err := admin.Get(srv.URL + "/item/o")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("reserve step of an owned gift: HTTP %d, want 404", resp.StatusCode)
	}
}
