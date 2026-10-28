package main

import (
	"context"
	"log"
	"time"
)

// The daily price check runs at this local hour (TZ of the container), after
// the Pi's own updates at 02:00–05:00.
const priceRefreshHour = 6

// nextPriceRefresh is the next priceRefreshHour:00 after now, in now's zone.
func nextPriceRefresh(now time.Time) time.Time {
	next := time.Date(now.Year(), now.Month(), now.Day(), priceRefreshHour, 0, 0, 0, now.Location())
	if !next.After(now) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}

// priceRefresher re-reads the price of every active gift once a day. When the
// last full pass is more than a day old (the Pi was off at 06:00), it runs one
// shortly after start instead of waiting for the next morning.
func (a *App) priceRefresher(ctx context.Context) {
	// Exchange rates (for sorting by price) are read at start and before
	// every price check.
	a.refreshRates(ctx)
	var last time.Time
	a.store.view(func(d *Data) { last = d.PricesCheckedAt })
	wait := time.Until(nextPriceRefresh(time.Now()))
	if time.Since(last) > 24*time.Hour {
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
	type target struct{ id, url string }
	var targets []target
	a.store.view(func(d *Data) {
		for _, it := range d.Items {
			if it.Status == statusActive {
				targets = append(targets, target{it.ID, it.URL})
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
			failed++
			continue
		}
		if meta.Price == "" {
			if meta.Unavailable {
				a.store.update(func(d *Data) error {
					it := d.item(tg.id)
					if it == nil || it.Unavailable {
						return errNoChange
					}
					log.Printf("price %s: now unavailable in the shop (%s)", siteName(it.URL), it.Title)
					it.Unavailable = true
					return nil
				})
				unavailable++
			} else {
				failed++
			}
			continue
		}
		now := time.Now()
		a.store.update(func(d *Data) error {
			it := d.item(tg.id)
			if it == nil {
				return errNoChange
			}
			if it.Price == meta.Price && it.Currency == meta.Currency {
				unchanged++
			} else {
				log.Printf("price %s: %s → %s (%s)", siteName(it.URL), formatPrice(it.Price, it.Currency), formatPrice(meta.Price, meta.Currency), it.Title)
				it.Price, it.Currency = meta.Price, meta.Currency
				changed++
			}
			if meta.Format != "" {
				it.Format = meta.Format
			}
			if it.Unavailable {
				log.Printf("price %s: back in the shop at %s (%s)", siteName(it.URL), formatPrice(meta.Price, meta.Currency), it.Title)
				it.Unavailable = false
			}
			it.PriceCheckedAt = &now
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
