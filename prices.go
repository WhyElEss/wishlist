package main

import (
	"context"
	"log"
	"time"
)

// nextPriceRefresh is the next full hour after now, in now's zone.
func nextPriceRefresh(now time.Time) time.Time {
	return time.Date(now.Year(), now.Month(), now.Day(), now.Hour()+1, 0, 0, 0, now.Location())
}

// priceRefresher re-reads the price of every active gift at every full hour.
// When the last full pass is more than an hour old (the app was down), it
// runs one shortly after start instead of waiting for the next hour.
func (a *App) priceRefresher(ctx context.Context) {
	// Exchange rates (for sorting by price) are read at start and before
	// every price check.
	a.refreshRates(ctx)
	var last time.Time
	a.store.view(func(d *Data) { last = d.PricesCheckedAt })
	wait := time.Until(nextPriceRefresh(time.Now()))
	if time.Since(last) > time.Hour {
		wait = time.Minute
	}
	for {
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
		a.refreshRates(ctx)
		a.refreshPrices(ctx)
		wait = time.Until(nextPriceRefresh(time.Now()))
	}
}

// refreshPrices updates only the price: a page that gives none (shop refused,
// out of stock) leaves the last known price in place, manual ones included.
func (a *App) refreshPrices(ctx context.Context) {
	type target struct{ id, url, title string }
	var targets []target
	a.store.view(func(d *Data) {
		for _, it := range d.Items {
			if it.Status == statusActive {
				targets = append(targets, target{it.ID, it.URL, it.Title})
			}
		}
	})

	start := time.Now()
	var changed, unchanged, unavailable, failed int
	for i, tg := range targets {
		if i > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(a.priceRefreshPause):
			}
		}
		fctx, cancel := context.WithTimeout(ctx, 60*time.Second)
		meta, err := a.readShop(fctx, tg.url)
		cancel()
		if err != nil {
			log.Printf("price %s: not read: %v (%s)", siteName(tg.url), err, tg.title)
			failed++
			continue
		}
		if meta.Price == "" && !meta.Unavailable {
			log.Printf("price %s: no price on the page (%s)", siteName(tg.url), tg.title)
			failed++
			continue
		}
		// An out-of-stock page may still show a price (sony.ca): it is
		// stored, and stays hidden from guests while the item is unavailable.
		now := time.Now()
		a.store.update(func(d *Data) error {
			it := d.item(tg.id)
			if it == nil {
				return errNoChange
			}
			if meta.Price != "" {
				same := it.Price == meta.Price && it.Currency == meta.Currency
				if !same {
					log.Printf("price %s: %s → %s (%s)", siteName(it.URL), formatPrice(it.Price, it.Currency), formatPrice(meta.Price, meta.Currency), it.Title)
					it.Price, it.Currency = meta.Price, meta.Currency
				}
				switch {
				case meta.Unavailable: // counted below
				case same:
					unchanged++
				default:
					changed++
				}
				it.PriceCheckedAt = &now
			}
			if meta.Format != "" {
				it.Format = meta.Format
			}
			switch {
			case meta.Unavailable && !it.Unavailable:
				log.Printf("price %s: now unavailable in the shop (%s)", siteName(it.URL), it.Title)
				it.Unavailable = true
			case !meta.Unavailable && it.Unavailable:
				log.Printf("price %s: back in the shop at %s (%s)", siteName(it.URL), formatPrice(meta.Price, meta.Currency), it.Title)
				it.Unavailable = false
			}
			if meta.Unavailable {
				unavailable++
			}
			return nil
		})
	}
	a.store.update(func(d *Data) error {
		d.PricesCheckedAt = time.Now()
		return nil
	})
	log.Printf("price check: %d gifts, %d changed, %d unchanged, %d unavailable in the shop, %d without a price from the shop, %s",
		len(targets), changed, unchanged, unavailable, failed, time.Since(start).Round(time.Second))
}
