package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

const (
	maxPageBytes  = 4 << 20
	maxImageBytes = 8 << 20
	browserUA     = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36"
)

// Pages are tried with a browser UA first and then as the Facebook link
// preview crawler, which some shops answer with OpenGraph tags even when they
// refuse ordinary scripts.
var userAgents = []string{
	browserUA,
	"facebookexternalhit/1.1 (+http://www.facebook.com/externalhit_uatext.php)",
}

type Meta struct {
	Title    string
	Image    string
	Price    string
	Currency string
	FinalURL *url.URL
}

func (m Meta) complete() bool { return m.Title != "" && m.Image != "" && m.Price != "" }

func (m Meta) missing() []string {
	var out []string
	if m.Title == "" {
		out = append(out, "название")
	}
	if m.Image == "" {
		out = append(out, "картинка")
	}
	if m.Price == "" {
		out = append(out, "цена")
	}
	return out
}

func mergeMeta(a, b Meta) Meta {
	if a.Title == "" {
		a.Title = b.Title
	}
	if a.Image == "" {
		a.Image = b.Image
	}
	if a.Price == "" {
		a.Price, a.Currency = b.Price, b.Currency
	} else if a.Currency == "" {
		a.Currency = b.Currency
	}
	if a.FinalURL == nil {
		a.FinalURL = b.FinalURL
	}
	return a
}

// fetchMeta returns what it could find; err is set only when every attempt
// failed outright.
func fetchMeta(ctx context.Context, c *http.Client, raw string) (Meta, error) {
	var best Meta
	var errs []error
	for _, ua := range userAgents {
		m, err := fetchMetaUA(ctx, c, raw, ua)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		best = mergeMeta(best, m)
		if best.complete() {
			break
		}
	}
	if best.Title == "" && best.Image == "" && best.Price == "" && len(errs) > 0 {
		return best, errors.Join(errs...)
	}
	return best, nil
}

func fetchMetaUA(ctx context.Context, c *http.Client, raw, ua string) (Meta, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return Meta{}, err
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "text/html,application/xhtml+xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	resp, err := c.Do(req)
	if err != nil {
		return Meta{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return Meta{}, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPageBytes))
	if err != nil {
		return Meta{}, err
	}
	m := parseMeta(string(body), resp.Request.URL)
	m.FinalURL = resp.Request.URL
	return m, nil
}

var (
	reMeta   = regexp.MustCompile(`(?is)<meta\b[^>]*>`)
	reLink   = regexp.MustCompile(`(?is)<link\b[^>]*>`)
	reAttr   = regexp.MustCompile(`(?s)([a-zA-Z_:.\-]+)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'>]+))`)
	reTitle  = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	reLDJSON = regexp.MustCompile(`(?is)<script\b[^>]*type\s*=\s*["']?application/ld\+json["']?[^>]*>(.*?)</script>`)
	reSpace  = regexp.MustCompile(`\s+`)
)

func attrs(tag string) map[string]string {
	m := map[string]string{}
	for _, a := range reAttr.FindAllStringSubmatch(tag, -1) {
		k := strings.ToLower(a[1])
		if _, ok := m[k]; !ok {
			m[k] = a[2] + a[3] + a[4]
		}
	}
	return m
}

func clean(s string) string {
	return strings.TrimSpace(reSpace.ReplaceAllString(html.UnescapeString(s), " "))
}

func parseMeta(doc string, base *url.URL) Meta {
	props := map[string]string{}
	for _, tag := range reMeta.FindAllString(doc, -1) {
		a := attrs(tag)
		content, ok := a["content"]
		if !ok {
			continue
		}
		for _, k := range []string{"property", "name", "itemprop"} {
			key := strings.ToLower(a[k])
			if key == "" {
				continue
			}
			if _, seen := props[key]; !seen {
				props[key] = clean(content)
			}
		}
	}
	first := func(keys ...string) string {
		for _, k := range keys {
			if v := props[k]; v != "" {
				return v
			}
		}
		return ""
	}

	var m Meta
	m.Title = first("og:title", "twitter:title")
	m.Image = first("og:image:secure_url", "og:image", "og:image:url", "twitter:image", "twitter:image:src", "image")
	m.Price = first("product:price:amount", "og:price:amount", "price")
	m.Currency = first("product:price:currency", "og:price:currency", "pricecurrency")

	m = mergeMeta(m, parseLD(doc))

	if m.Image == "" {
		for _, tag := range reLink.FindAllString(doc, -1) {
			if a := attrs(tag); strings.EqualFold(a["rel"], "image_src") && a["href"] != "" {
				m.Image = clean(a["href"])
				break
			}
		}
	}
	if m.Title == "" {
		if t := reTitle.FindStringSubmatch(doc); t != nil {
			m.Title = clean(t[1])
		}
	}
	m.Image = resolveURL(base, m.Image)
	m.Price = strings.TrimSpace(m.Price)
	m.Currency = strings.ToUpper(strings.TrimSpace(m.Currency))
	return m
}

func resolveURL(base *url.URL, ref string) string {
	if ref == "" {
		return ""
	}
	u, err := url.Parse(ref)
	if err != nil {
		return ""
	}
	if base != nil {
		u = base.ResolveReference(u)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return ""
	}
	return u.String()
}

// parseLD looks for a schema.org Product in the page's JSON-LD blocks.
func parseLD(doc string) Meta {
	var m Meta
	for _, match := range reLDJSON.FindAllStringSubmatch(doc, -1) {
		var v any
		if json.Unmarshal([]byte(strings.TrimSpace(match[1])), &v) != nil {
			continue
		}
		walkLD(v, &m)
		if m.Title != "" && m.Price != "" {
			break
		}
	}
	return m
}

func walkLD(v any, m *Meta) {
	switch x := v.(type) {
	case []any:
		for _, e := range x {
			walkLD(e, m)
		}
	case map[string]any:
		if isLDType(x["@type"], "Product") || isLDType(x["@type"], "ProductGroup") {
			if m.Title == "" {
				m.Title = clean(ldString(x["name"]))
			}
			if m.Image == "" {
				m.Image = ldImage(x["image"])
			}
			if m.Price == "" {
				m.Price, m.Currency = ldOffer(x["offers"])
			}
			if m.Price == "" {
				if vs, ok := x["hasVariant"].([]any); ok && len(vs) > 0 {
					if v0, ok := vs[0].(map[string]any); ok {
						m.Price, m.Currency = ldOffer(v0["offers"])
					}
				}
			}
		}
		walkLD(x["@graph"], m)
		walkLD(x["mainEntity"], m)
	}
}

func isLDType(v any, want string) bool {
	switch t := v.(type) {
	case string:
		return t == want || t == "http://schema.org/"+want || t == "https://schema.org/"+want
	case []any:
		for _, e := range t {
			if isLDType(e, want) {
				return true
			}
		}
	}
	return false
}

func ldString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	}
	return ""
}

func ldImage(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []any:
		for _, e := range t {
			if s := ldImage(e); s != "" {
				return s
			}
		}
	case map[string]any:
		if s := ldString(t["url"]); s != "" {
			return s
		}
		return ldString(t["contentUrl"])
	}
	return ""
}

func ldOffer(v any) (price, currency string) {
	switch t := v.(type) {
	case []any:
		for _, e := range t {
			if p, c := ldOffer(e); p != "" {
				return p, c
			}
		}
	case map[string]any:
		price = ldString(t["price"])
		if price == "" {
			price = ldString(t["lowPrice"])
		}
		currency = ldString(t["priceCurrency"])
		if price == "" {
			if spec, ok := t["priceSpecification"]; ok {
				return ldOffer(spec)
			}
			if inner, ok := t["offers"]; ok {
				return ldOffer(inner)
			}
		}
		if currency == "" {
			if spec, ok := t["priceSpecification"].(map[string]any); ok {
				currency = ldString(spec["priceCurrency"])
			}
		}
	}
	return price, currency
}

var imageExt = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/webp": ".webp",
	"image/gif":  ".gif",
	"image/avif": ".avif",
}

func downloadImage(ctx context.Context, c *http.Client, src, referer string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", browserUA)
	req.Header.Set("Accept", "image/avif,image/webp,image/png,image/jpeg,image/*;q=0.8")
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("image: HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxImageBytes+1))
	if err != nil {
		return nil, "", err
	}
	if len(b) > maxImageBytes {
		return nil, "", errors.New("image is larger than 8 MB")
	}
	ct, _, _ := strings.Cut(resp.Header.Get("Content-Type"), ";")
	ext, ok := imageExt[strings.ToLower(strings.TrimSpace(ct))]
	if !ok {
		ext, ok = imageExt[http.DetectContentType(b)]
	}
	if !ok {
		return nil, "", fmt.Errorf("not an image (%s)", ct)
	}
	return b, ext, nil
}

// Shops whose .com site ships within the US and .ca site within Canada. For
// anything else the region is left empty and the visitor's country decides.
var regionalShops = []string{"amazon", "ebay", "walmart", "bestbuy", "target", "costco", "newegg", "homedepot"}

func detectRegion(u *url.URL) string {
	if u == nil {
		return ""
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	for _, shop := range regionalShops {
		switch {
		case host == shop+".com" || strings.HasSuffix(host, "."+shop+".com"):
			return "US"
		case host == shop+".ca" || strings.HasSuffix(host, "."+shop+".ca"):
			return "CA"
		}
	}
	return ""
}

func siteName(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
}
