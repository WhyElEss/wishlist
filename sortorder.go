package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Sort orders of the gift list (Settings.SortOrder). "" is the admin's own
// drag order.
var sortOrders = []string{"", "date_desc", "date_asc", "title_asc", "title_desc", "price_asc", "price_desc"}

func validSortOrder(s string) bool { return slices.Contains(sortOrders, s) }

// sortedItems returns the items in the given order; ties keep the stored
// order. Grouping (★, reserved) is left to the callers.
func sortedItems(items []*Item, order string, rates map[string]float64) []*Item {
	out := slices.Clone(items)
	if order == "" {
		return out
	}
	field, dir, _ := strings.Cut(order, "_")
	desc := dir == "desc"
	slices.SortStableFunc(out, func(a, b *Item) int {
		switch field {
		case "date":
			return flip(a.CreatedAt.Compare(b.CreatedAt), desc)
		case "title":
			// Latin titles first and Cyrillic after them in both directions;
			// the direction applies within each group.
			ga, gb := scriptGroup(a.Title), scriptGroup(b.Title)
			if ga != gb {
				return ga - gb
			}
			return flip(strings.Compare(titleKey(a.Title), titleKey(b.Title)), desc)
		case "price":
			// Gifts without a comparable price go last in both directions.
			pa, oka := priceCAD(a, rates)
			pb, okb := priceCAD(b, rates)
			switch {
			case !oka && !okb:
				return 0
			case !oka:
				return 1
			case !okb:
				return -1
			}
			return flip(cmpFloat(pa, pb), desc)
		}
		return 0
	})
	return out
}

func flip(c int, desc bool) int {
	if desc {
		return -c
	}
	return c
}

func cmpFloat(a, b float64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// scriptGroup is 0 for a title whose first letter is Latin, 1 for Cyrillic
// and 2 for anything else (other scripts, no letters at all).
func scriptGroup(title string) int {
	for _, r := range title {
		switch {
		case unicode.Is(unicode.Latin, r):
			return 0
		case unicode.Is(unicode.Cyrillic, r):
			return 1
		case unicode.IsLetter(r):
			return 2
		}
	}
	return 2
}

// foldLetters maps accented Latin letters to their base letter and ё to е, so
// "Elán" sorts with "Elan" and «Ёлка» with «Елка».
var foldLetters = strings.NewReplacer(
	"à", "a", "á", "a", "â", "a", "ã", "a", "ä", "a", "å", "a", "ą", "a",
	"ç", "c", "č", "c", "ć", "c", "ď", "d",
	"è", "e", "é", "e", "ê", "e", "ë", "e", "ě", "e", "ę", "e",
	"ì", "i", "í", "i", "î", "i", "ï", "i",
	"ł", "l", "ñ", "n", "ň", "n", "ń", "n",
	"ò", "o", "ó", "o", "ô", "o", "õ", "o", "ö", "o", "ø", "o",
	"ř", "r", "š", "s", "ś", "s", "ť", "t",
	"ù", "u", "ú", "u", "û", "u", "ü", "u", "ů", "u",
	"ý", "y", "ÿ", "y", "ž", "z", "ź", "z", "ż", "z",
	"ё", "е",
)

func titleKey(title string) string {
	return foldLetters.Replace(strings.ToLower(strings.TrimSpace(title)))
}

// priceCAD is the gift's price in Canadian dollars for sorting. Gifts the
// shop does not sell right now, without a price, or in a currency without a
// rate have none.
func priceCAD(it *Item, rates map[string]float64) (float64, bool) {
	if it.Unavailable || it.Price == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(strings.ReplaceAll(it.Price, ",", ""), 64)
	if err != nil {
		return 0, false
	}
	cur := it.Currency
	if cur == "" || cur == "CAD" {
		return v, true
	}
	rate, ok := rates[cur]
	if !ok {
		return 0, false
	}
	return v * rate, true
}

// refreshRates reads the Bank of Canada daily average rate to CAD of every
// other currency the gifts use. A failed read keeps the previous rate.
func (a *App) refreshRates(ctx context.Context) {
	var curs []string
	a.store.view(func(d *Data) {
		for _, it := range d.Items {
			if c := it.Currency; c != "" && c != "CAD" && !slices.Contains(curs, c) {
				curs = append(curs, c)
			}
		}
	})
	rates := map[string]float64{}
	var date string
	for _, cur := range curs {
		v, d, err := a.readRate(ctx, cur)
		if err != nil {
			log.Printf("rate %s: %v", cur, err)
			continue
		}
		rates[cur], date = v, d
	}
	if len(rates) == 0 {
		return
	}
	a.store.update(func(d *Data) error {
		if d.Rates == nil {
			d.Rates = map[string]float64{}
		}
		for cur, v := range rates {
			d.Rates[cur] = v
		}
		d.RatesDate = date
		return nil
	})
	log.Printf("rates to CAD on %s: %v", date, rates)
}

// readRate is one currency's latest daily rate to CAD from the Bank of
// Canada Valet API (series FX<cur>CAD, no key needed).
func (a *App) readRate(ctx context.Context, cur string) (float64, string, error) {
	if len(cur) != 3 || strings.ToUpper(cur) != cur {
		return 0, "", fmt.Errorf("bad currency %q", cur)
	}
	series := "FX" + cur + "CAD"
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.ratesBase+"/observations/"+series+"/json?recent=1", nil)
	if err != nil {
		return 0, "", err
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var body struct {
		Observations []map[string]json.RawMessage `json:"observations"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return 0, "", err
	}
	if len(body.Observations) == 0 {
		return 0, "", fmt.Errorf("no observations")
	}
	obs := body.Observations[len(body.Observations)-1]
	var date string
	var val struct {
		V string `json:"v"`
	}
	json.Unmarshal(obs["d"], &date)
	if err := json.Unmarshal(obs[series], &val); err != nil {
		return 0, "", fmt.Errorf("no %s value", series)
	}
	v, err := strconv.ParseFloat(val.V, 64)
	if err != nil || v <= 0 {
		return 0, "", fmt.Errorf("bad value %q", val.V)
	}
	return v, date, nil
}
