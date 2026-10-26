package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestUSZip(t *testing.T) {
	for _, c := range []struct{ addr, want string }{
		{"John Doe\nSuite 12345\n123 Main St\nPortland, OR 97230-1234\nUSA", "97230"},
		{"Wilmington, DE 19801", "19801"},
		{"", "10001"},
	} {
		if got := usZip(c.addr); got != c.want {
			t.Errorf("usZip(%q) = %q, want %q", c.addr, got, c.want)
		}
	}
}

// fakeAmazon mimics the delivery-location flow measured on amazon.com: no
// price until the session's zip is set through the address dialog.
func fakeAmazon(t *testing.T, dialogs *int32) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/dp/B000TEST01":
			if c, err := r.Cookie("zip"); err == nil {
				w.Write([]byte(`<html><head><title>Amazon.com: Test Charger</title></head><body>
<span class="nav-line-2" id="glow-ingress-line2">
  New York ` + c.Value + `&zwnj;</span>
<span id="productTitle" class="a-size-large">   Test Charger   </span>
<input type="hidden" id="twister-plus-price-data-price" value="19.99" />
<input type="hidden" id="twister-plus-price-data-price-unit" value="$" /></body></html>`))
				return
			}
			w.Write([]byte(`<html><head><title>Amazon.com: Test Charger</title></head><body>
<span id="glow-ingress-line2">Canada</span>
<span data-a-modal='{&quot;ajaxHeaders&quot;:{&quot;anti-csrftoken-a2z&quot;:&quot;page+tok&#x3D;&quot;}}'></span>
<div id="outOfStock">This item cannot be shipped to your selected delivery location.</div></body></html>`))
		case "/portal-migration/hz/glow/get-rendered-address-selections":
			atomic.AddInt32(dialogs, 1)
			if r.Header.Get("anti-csrftoken-a2z") != "page+tok=" {
				http.Error(w, "bad token", http.StatusForbidden)
				return
			}
			w.Write([]byte(`<script>P.declare("GLUXConfig", { CSRF_TOKEN : "dialog-tok", IDs:{}});</script>`))
		case "/portal-migration/hz/glow/address-change":
			if r.Method != http.MethodPost || r.Header.Get("anti-csrftoken-a2z") != "dialog-tok" {
				http.Error(w, "bad request", http.StatusForbidden)
				return
			}
			var body struct{ ZipCode string }
			json.NewDecoder(r.Body).Decode(&body)
			http.SetCookie(w, &http.Cookie{Name: "zip", Value: body.ZipCode, Path: "/"})
			w.Write([]byte(`{"isValidAddress":1,"isAddressUpdated":1,"successful":1}`))
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestAmazonUSSetsZip(t *testing.T) {
	var dialogs int32
	srv := fakeAmazon(t, &dialogs)
	defer srv.Close()
	az := newAmazonUS(srv.URL, func() string { return "97230" })

	for i := 0; i < 2; i++ {
		m, err := az.fetchMeta(context.Background(), srv.URL+"/dp/B000TEST01")
		if err != nil {
			t.Fatal(err)
		}
		if m.Price != "19.99" || m.Currency != "USD" || m.Title != "Test Charger" {
			t.Fatalf("fetch %d: got %+v", i, m)
		}
	}
	if n := atomic.LoadInt32(&dialogs); n != 1 {
		t.Errorf("address dialog opened %d times, want 1 (zip should stick to the session)", n)
	}
}
