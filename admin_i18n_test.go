package main

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

var reCyrillic = regexp.MustCompile(`[А-Яа-яЁё]`)

// getLang fetches path with the given Accept-Language through client.
func getLang(t *testing.T, client *http.Client, rawURL, lang string) string {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, rawURL, nil)
	req.Header.Set("Accept-Language", lang)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return body(t, resp)
}

func TestAdminBilingual(t *testing.T) {
	srv, app := newTestApp(t)
	app.store.update(func(d *Data) error {
		d.Settings.TitleRU, d.Settings.IntroRU = "W", "Hi"
		d.Items = []*Item{
			{ID: "g", URL: "https://shop.example/g", Title: "Gift", Quantity: 1, Status: statusActive,
				Fetch: &FetchInfo{Missing: []string{"price"}, ImageErr: "timeout"}},
			{ID: "old", URL: "https://shop.example/old", Title: "Old", Quantity: 1, Status: statusOwned, FetchNote: "legacy note"},
		}
		return nil
	})

	// The login page follows the browser and has a language switch.
	anon := newClient(t)
	if page := getLang(t, anon, srv.URL+"/admin/login", "en-US"); !strings.Contains(page, "Admin sign-in") || !strings.Contains(page, `href="/lang/ru?next=%2fadmin%2flogin"`) {
		t.Error("English login page")
	}
	if page := getLang(t, anon, srv.URL+"/admin/login", "ru"); !strings.Contains(page, "Вход в админку") {
		t.Error("Russian login page")
	}

	admin := newClient(t)
	admin.PostForm(srv.URL+"/admin/login", url.Values{"password": {"admin-password"}})
	pages := []string{"/admin", "/admin/items/g", "/admin/items/old", "/admin/settings", "/admin?m=saved"}
	for _, path := range pages {
		page := getLang(t, admin, srv.URL+path, "en-US,en;q=0.9")
		// "Русский" is the switch to Russian; everything else must be English.
		if c := reCyrillic.FindString(strings.ReplaceAll(page, "Русский", "")); c != "" {
			i := strings.Index(page, c)
			t.Errorf("English %s has Russian text near %q", path, page[max(0, i-40):min(len(page), i+80)])
		}
		if !strings.Contains(page, ">Settings</a>") || !strings.Contains(page, "Log out") {
			t.Errorf("English %s lacks the admin nav", path)
		}
	}
	for _, path := range pages {
		if page := getLang(t, admin, srv.URL+path, "ru"); !strings.Contains(page, ">Настройки</a>") || !strings.Contains(page, ">English</a>") {
			t.Errorf("Russian %s lacks the admin nav or the language switch", path)
		}
	}

	en := getLang(t, admin, srv.URL+"/admin/items/g", "en")
	for _, want := range []string{"Could not get from the page: price. Fill it in by hand.", "Could not download the picture: timeout.", "← All gifts", "Ship to"} {
		if !strings.Contains(en, want) {
			t.Errorf("English edit page lacks %q", want)
		}
	}
	ru := getLang(t, admin, srv.URL+"/admin/items/g", "ru")
	if !strings.Contains(ru, "Со страницы не удалось взять: цена. Допишите вручную.") {
		t.Error("Russian fetch note missing")
	}
	if page := getLang(t, admin, srv.URL+"/admin/items/old", "en"); !strings.Contains(page, "legacy note") {
		t.Error("an older stored note is no longer shown")
	}
	if page := getLang(t, admin, srv.URL+"/admin?m=saved", "en"); !strings.Contains(page, `<div class="flash ok">Saved.</div>`) {
		t.Error("flash message not translated")
	}
	if page := getLang(t, admin, srv.URL+"/admin?m=nonsense", "en"); strings.Contains(page, `flash ok`) {
		t.Error("unknown ?m= shown as a flash")
	}

	// The switch sets a cookie that wins over the browser's language.
	if page := getLang(t, admin, srv.URL+"/lang/ru?next=/admin/settings", "en"); !strings.Contains(page, "<h1>Настройки</h1>") {
		t.Error("language switch does not apply to the admin panel")
	}
}

func TestAdminStatusStillValidated(t *testing.T) {
	srv, app := newTestApp(t)
	app.store.update(func(d *Data) error {
		d.Items = []*Item{{ID: "g", URL: "https://shop.example/g", Title: "Gift", Quantity: 1, Status: statusActive}}
		return nil
	})
	admin := newClient(t)
	admin.PostForm(srv.URL+"/admin/login", url.Values{"password": {"admin-password"}})
	for status, want := range map[string]string{"owned": statusOwned, "bogus": statusActive} {
		admin.PostForm(srv.URL+"/admin/items/g", url.Values{"url": {"https://shop.example/g"}, "status": {status}})
		var got string
		app.store.view(func(d *Data) { got = d.item("g").Status })
		if got != want {
			t.Errorf("status %q saved as %q, want %q", status, got, want)
		}
	}
}
