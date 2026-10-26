package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

// amazonUS fetches amazon.com pages as a visitor with a US delivery zip.
//
// Seen from the Pi in Canada, amazon.com product pages say "This item cannot
// be shipped to your selected delivery location" and carry no price. Setting a
// US zip through the same "Deliver to" dialog the site uses (three requests:
// product page → dialog → address-change) brings the price back; the zip then
// sticks to the session cookies, so the dialog is only needed once per session.
type amazonUS struct {
	origin string        // https://www.amazon.com; a test server in tests
	zip    func() string // US zip to deliver to

	mu     sync.Mutex
	client *http.Client
}

func newAmazonUS(origin string, zip func() string) *amazonUS {
	jar, _ := cookiejar.New(nil)
	return &amazonUS{origin: origin, zip: zip, client: &http.Client{Jar: jar, Timeout: 25 * time.Second}}
}

func isAmazonUS(u *url.URL) bool {
	if u == nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "amazon.com" || host == "www.amazon.com" || host == "smile.amazon.com"
}

var (
	reAmazonAntiCSRF = regexp.MustCompile(`anti-csrftoken-a2z&quot;:&quot;([^"<>]{1,200}?)&quot;`)
	reAmazonGlowCSRF = regexp.MustCompile(`CSRF_TOKEN\s*:\s*"([^"]+)"`)
	reAmazonDeliver  = regexp.MustCompile(`id="glow-ingress-line2"[^>]*>\s*([^<]*)`)
	reAmazonPrice    = regexp.MustCompile(`id="twister-plus-price-data-price" value="([0-9][0-9.,]*)"`)
	reAmazonTitle    = regexp.MustCompile(`id="productTitle"[^>]*>([^<]+)<`)
	reAmazonUnit     = regexp.MustCompile(`id="twister-plus-price-data-price-unit" value="([^"]*)"`)
	reUSZip          = regexp.MustCompile(`\b(\d{5})(?:-\d{4})?\b`)
)

// usZip takes the last 5-digit group of the US address (US addresses end with
// the ZIP code); 10001 when there is none.
func usZip(address string) string {
	m := reUSZip.FindAllStringSubmatch(address, -1)
	if len(m) == 0 {
		return "10001"
	}
	return m[len(m)-1][1]
}

// usZip is the zip of the US shipping address in the settings.
func (s *Store) usZip() string {
	var addr string
	s.view(func(d *Data) { addr = d.Settings.AddressUS })
	return usZip(addr)
}

func (az *amazonUS) get(ctx context.Context, raw string, header map[string]string) (string, *url.URL, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("User-Agent", browserUA)
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := az.client.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxPageBytes))
	return string(b), resp.Request.URL, err
}

// deliversTo reports whether the page header already shows the zip.
func deliversTo(page, zip string) bool {
	m := reAmazonDeliver.FindStringSubmatch(page)
	return m != nil && strings.Contains(m[1], zip)
}

// setZip changes the session's delivery location, starting from a product page.
func (az *amazonUS) setZip(ctx context.Context, page, referer, zip string) error {
	m := reAmazonAntiCSRF.FindStringSubmatch(page)
	if m == nil {
		return errors.New("amazon: no anti-csrftoken on the page")
	}
	dialog, _, err := az.get(ctx, az.origin+"/portal-migration/hz/glow/get-rendered-address-selections?deviceType=desktop&pageType=Detail&storeContext=amazon-home&actionSource=desktop-modal",
		map[string]string{"anti-csrftoken-a2z": html.UnescapeString(m[1])})
	if err != nil {
		return fmt.Errorf("amazon address dialog: %w", err)
	}
	t := reAmazonGlowCSRF.FindStringSubmatch(dialog)
	if t == nil {
		return errors.New("amazon: no CSRF_TOKEN in the address dialog")
	}
	body, _ := json.Marshal(map[string]string{
		"locationType": "LOCATION_INPUT", "zipCode": zip, "deviceType": "web",
		"storeContext": "generic", "pageType": "Detail", "actionSource": "glow",
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, az.origin+"/portal-migration/hz/glow/address-change?actionSource=glow", strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", browserUA)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("anti-csrftoken-a2z", t[1])
	req.Header.Set("Origin", az.origin)
	req.Header.Set("Referer", referer)
	resp, err := az.client.Do(req)
	if err != nil {
		return fmt.Errorf("amazon address-change: %w", err)
	}
	defer resp.Body.Close()
	var res struct {
		Successful       int `json:"successful"`
		IsAddressUpdated int `json:"isAddressUpdated"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&res); err != nil {
		return fmt.Errorf("amazon address-change: HTTP %d: %w", resp.StatusCode, err)
	}
	if res.Successful != 1 || res.IsAddressUpdated != 1 {
		return fmt.Errorf("amazon address-change refused zip %s", zip)
	}
	return nil
}

func (az *amazonUS) fetchMeta(ctx context.Context, raw string) (Meta, error) {
	// One at a time: requests share the session and its delivery location.
	az.mu.Lock()
	defer az.mu.Unlock()

	zip := az.zip()
	page, final, err := az.get(ctx, raw, nil)
	if err != nil {
		return Meta{}, err
	}
	var zipErr error
	if !deliversTo(page, zip) {
		if zipErr = az.setZip(ctx, page, raw, zip); zipErr == nil {
			page, final, err = az.get(ctx, raw, nil)
			if err != nil {
				return Meta{}, err
			}
		}
	}
	m := parseMeta(page, final)
	m.FinalURL = final
	// <title> reads "Amazon.com: <name>"; the product title element has the name alone.
	if t := reAmazonTitle.FindStringSubmatch(page); t != nil && clean(t[1]) != "" {
		m.Title = clean(t[1])
	}
	if m.Price == "" {
		if p := reAmazonPrice.FindStringSubmatch(page); p != nil {
			m.Price = p[1]
			if u := reAmazonUnit.FindStringSubmatch(page); u != nil && u[1] == "$" {
				m.Currency = "USD"
			}
		}
	}
	if zipErr != nil {
		log.Printf("%v", zipErr)
	}
	return m, nil
}

// fetchMeta picks the fetcher for the shop.
func (a *App) fetchMeta(ctx context.Context, raw string) (Meta, error) {
	if u, err := url.Parse(raw); err == nil && isAmazonUS(u) {
		return a.amazon.fetchMeta(ctx, raw)
	}
	return fetchMeta(ctx, a.client, raw)
}
