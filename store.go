package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sync"
	"time"
)

const (
	statusActive   = "active"   // shown to visitors
	statusReceived = "received" // gift arrived; archived
	statusOwned    = "owned"    // got it elsewhere; hidden

	defaultReserveDays = 14
)

var (
	errNotFound    = errors.New("not found")
	errUnavailable = errors.New("unavailable")
	errNoChange    = errors.New("no change")
)

type Settings struct {
	TitleRU   string `json:"title_ru"`
	TitleEN   string `json:"title_en"`
	IntroRU   string `json:"intro_ru"`
	IntroEN   string `json:"intro_en"`
	AddressUS string `json:"address_us"`
	AddressCA string `json:"address_ca"`
	// ShowReservations is the spoiler switch: off hides who reserved what from
	// the admin panel and from the Telegram notifications.
	ShowReservations bool `json:"show_reservations"`
	ReserveDays      int  `json:"reserve_days"`
}

func (s Settings) title(lang string) string {
	if lang == "ru" {
		return s.TitleRU
	}
	return s.TitleEN
}

func (s Settings) intro(lang string) string {
	if lang == "ru" {
		return s.IntroRU
	}
	return s.IntroEN
}

func (s Settings) address(region string) string {
	if region == "US" {
		return s.AddressUS
	}
	return s.AddressCA
}

type Reservation struct {
	ID    string `json:"id"`
	Token string `json:"token"`
	// Email is optional and only kept until the gift is marked as bought.
	Email        string     `json:"email,omitempty"`
	Lang         string     `json:"lang"`
	CreatedAt    time.Time  `json:"created_at"`
	ExpiresAt    time.Time  `json:"expires_at"`
	PurchasedAt  *time.Time `json:"purchased_at,omitempty"`
	ReminderSent bool       `json:"reminder_sent,omitempty"`
}

// live reports whether the reservation still holds its unit of the item.
func (r *Reservation) live(now time.Time) bool {
	return r.PurchasedAt != nil || now.Before(r.ExpiresAt)
}

type Item struct {
	ID       string `json:"id"`
	URL      string `json:"url"`
	Title    string `json:"title"`
	Image    string `json:"image,omitempty"`     // file name in the images dir
	ImageSrc string `json:"image_src,omitempty"` // where it was downloaded from
	Price    string `json:"price,omitempty"`
	Currency string `json:"currency,omitempty"`
	// Region picks the shipping address: "US", "CA", or "" for "by the
	// visitor's country".
	Region       string         `json:"region,omitempty"`
	Notes        string         `json:"notes,omitempty"`
	Priority     int            `json:"priority,omitempty"`
	Quantity     int            `json:"quantity"`
	Status       string         `json:"status"`
	FetchNote    string         `json:"fetch_note,omitempty"`
	Reservations []*Reservation `json:"reservations,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

func (it *Item) reserved(now time.Time) (live, purchased int) {
	for _, r := range it.Reservations {
		if r.live(now) {
			live++
		}
		if r.PurchasedAt != nil {
			purchased++
		}
	}
	return live, purchased
}

func (it *Item) left(now time.Time) int {
	live, _ := it.reserved(now)
	if n := it.Quantity - live; n > 0 {
		return n
	}
	return 0
}

func (it *Item) available(now time.Time) bool {
	return it.Status == statusActive && it.left(now) > 0
}

type Data struct {
	Settings Settings `json:"settings"`
	Items    []*Item  `json:"items"`
}

func (d *Data) item(id string) *Item {
	for _, it := range d.Items {
		if it.ID == id {
			return it
		}
	}
	return nil
}

func (d *Data) findReservation(token string) (*Item, *Reservation) {
	if token == "" {
		return nil, nil
	}
	for _, it := range d.Items {
		for _, r := range it.Reservations {
			if r.Token == token {
				return it, r
			}
		}
	}
	return nil, nil
}

func defaultSettings() Settings {
	return Settings{
		TitleRU:     "Мой вишлист",
		TitleEN:     "My wishlist",
		IntroRU:     "Здесь то, что я был бы рад получить. Выберите подарок и нажмите «Подарю это» — остальные увидят, что он уже занят, но не узнают кем.",
		IntroEN:     "Here is what I would be happy to get. Pick a gift and press “I’ll gift this” — others will see it is taken, but not by whom.",
		ReserveDays: defaultReserveDays,
	}
}

// Store keeps the whole data set in memory and rewrites the JSON file on every
// change. One mutex serialises all access, which is what makes a reservation
// atomic: the availability check and the write happen under the same lock.
type Store struct {
	mu   sync.Mutex
	path string
	data Data
}

func openStore(path string) (*Store, error) {
	s := &Store{path: path}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		s.data = Data{Settings: defaultSettings()}
		return s, s.saveLocked()
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &s.data); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if s.data.Settings.ReserveDays <= 0 {
		s.data.Settings.ReserveDays = defaultReserveDays
	}
	return s, nil
}

func (s *Store) saveLocked() error {
	b, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// view runs fn under the lock. fn must copy out whatever it needs.
func (s *Store) view(fn func(d *Data)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(&s.data)
}

// update runs fn under the lock and saves the result. fn must validate before
// mutating: an error leaves the data as fn left it, unsaved. errNoChange skips
// the save and is not reported.
func (s *Store) update(fn func(d *Data) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := fn(&s.data); err != nil {
		if errors.Is(err, errNoChange) {
			return nil
		}
		return err
	}
	return s.saveLocked()
}

func randomString(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func newID() string    { return randomString(6) }
func newToken() string { return randomString(24) }
