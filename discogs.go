package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// Discogs pages sit behind a Cloudflare JavaScript challenge (HTTP 403 "Just
// a moment..." for any plain client), but the public API at api.discogs.com
// answers without a key (25 requests a minute). Master and release links are
// read from it.
type discogsAPI struct {
	base   string // https://api.discogs.com; a test server in tests
	client *http.Client
}

// The API asks for a User-Agent that names the application.
const discogsUA = "WishlistBot/1.0 +https://github.com/WhyElEss/wishlist"

func isDiscogs(u *url.URL) bool {
	if u == nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "discogs.com" || host == "www.discogs.com"
}

// /master/239695-Various-..., /release/201415-..., optionally after a
// language prefix such as /fr/.
var reDiscogsPath = regexp.MustCompile(`^/(?:[a-z]{2}(?:-[a-z]{2})?/)?(master|release)/(\d+)(?:[-/]|$)`)

func discogsRef(u *url.URL) (kind, id string, ok bool) {
	m := reDiscogsPath.FindStringSubmatch(strings.ToLower(u.Path))
	if m == nil {
		return "", "", false
	}
	return m[1], m[2], true
}

func (d *discogsAPI) get(ctx context.Context, path string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.base+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", discogsUA)
	req.Header.Set("Accept", "application/json")
	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Discogs API: HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, maxPageBytes)).Decode(v)
}

type discogsItem struct {
	Title   string `json:"title"`
	Artists []struct {
		Name string `json:"name"`
	} `json:"artists"`
	Images []struct {
		Type string `json:"type"`
		URI  string `json:"uri"`
	} `json:"images"`
	LowestPrice *float64 `json:"lowest_price"`
}

// Discogs tells same-named artists apart with " (2)" and similar.
var reDiscogsArtistNo = regexp.MustCompile(`\s*\(\d+\)$`)

func (it discogsItem) name() string {
	var names []string
	for _, a := range it.Artists {
		names = append(names, reDiscogsArtistNo.ReplaceAllString(a.Name, ""))
	}
	if len(names) == 0 {
		return it.Title
	}
	return strings.Join(names, ", ") + " – " + it.Title
}

func (it discogsItem) image() string {
	for _, im := range it.Images {
		if im.Type == "primary" && im.URI != "" {
			return im.URI
		}
	}
	if len(it.Images) > 0 {
		return it.Images[0].URI
	}
	return ""
}

func formatAmount(v float64) string { return strconv.FormatFloat(v, 'f', 2, 64) }

// fetchMeta reads a master or release. The price is the lowest offer on the
// Discogs marketplace in USD (sellers worldwide, shipping not included).
func (d *discogsAPI) fetchMeta(ctx context.Context, u *url.URL) (Meta, error) {
	kind, id, ok := discogsRef(u)
	if !ok {
		return Meta{}, errors.New("Discogs: only master and release links can be read")
	}
	var it discogsItem
	if err := d.get(ctx, "/"+kind+"s/"+id, &it); err != nil {
		return Meta{}, err
	}
	m := Meta{Title: it.name(), Image: it.image(), FinalURL: u}

	switch kind {
	case "release":
		// The stats call names its currency in the answer; the release's own
		// lowest_price ignores curr_abbr without a signed-in user (measured).
		var st struct {
			LowestPrice *struct {
				Value    float64 `json:"value"`
				Currency string  `json:"currency"`
			} `json:"lowest_price"`
		}
		if err := d.get(ctx, "/marketplace/stats/"+id+"?curr_abbr=USD", &st); err == nil && st.LowestPrice != nil {
			m.Price, m.Currency = formatAmount(st.LowestPrice.Value), st.LowestPrice.Currency
		}
	case "master":
		// Masters have no stats call. Their lowest_price matched the USD
		// stats of the same release when measured (8.0 = 8.0 USD), so it is
		// taken as USD.
		if it.LowestPrice != nil {
			m.Price, m.Currency = formatAmount(*it.LowestPrice), "USD"
		}
	}
	return m, nil
}
