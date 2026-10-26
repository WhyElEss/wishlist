package main

import (
	"bufio"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// A server that offers no STARTTLS must never see the login.
func TestMailRefusesPlaintextAuth(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	got := make(chan string, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		r := bufio.NewReader(c)
		c.Write([]byte("220 fake ESMTP\r\n"))
		var seen []string
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				break
			}
			seen = append(seen, strings.TrimSpace(line))
			switch {
			case strings.HasPrefix(line, "EHLO"):
				c.Write([]byte("250-fake\r\n250 AUTH PLAIN\r\n"))
			case strings.HasPrefix(line, "QUIT"):
				c.Write([]byte("221 bye\r\n"))
			default:
				c.Write([]byte("250 ok\r\n"))
			}
		}
		got <- strings.Join(seen, "|")
	}()
	host, port, _ := net.SplitHostPort(ln.Addr().String())
	m := &mailer{host: host, port: port, user: "me@icloud.com", pass: "secret-app-password", from: "Wishlist <wishlist@example.com>", helo: "wishlist.example.com"}
	err = m.send("guest@example.com", "s", "b")
	if err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Fatalf("send without STARTTLS: err = %v", err)
	}
	seen := <-got
	if strings.Contains(seen, "AUTH") {
		t.Errorf("credentials sent without encryption: %s", seen)
	}
	if first, _, _ := strings.Cut(seen, "|"); first != "EHLO wishlist.example.com" {
		t.Errorf("client did not introduce itself by the site name: %s", seen)
	}
}

func TestMailUI(t *testing.T) {
	srv, app := newTestApp(t)
	app.cfg.SMTPFrom = "Wishlist <wishlist@example.com>"
	app.mail = &mailer{host: "127.0.0.1", port: "1", from: app.cfg.SMTPFrom} // nothing listens: sending fails quietly
	app.store.update(func(d *Data) error {
		d.Items = []*Item{{ID: "g", URL: "https://shop.example/g", Title: "Gift", Quantity: 1, Status: statusActive}}
		return nil
	})

	resp, _ := http.Get(srv.URL + "/item/g")
	if page := body(t, resp); !strings.Contains(page, `name="email"`) {
		t.Error("email field missing although mail is configured")
	}
	resp, err := newClient(t).PostForm(srv.URL+"/item/g/reserve", url.Values{"email": {"guest@example.com"}})
	if err != nil {
		t.Fatal(err)
	}
	if page := body(t, resp); !strings.Contains(page, "We have emailed you this link") {
		t.Error("reservation page does not say the link was emailed")
	}

	admin := newClient(t)
	admin.PostForm(srv.URL+"/admin/login", url.Values{"password": {"admin-password"}})
	resp, _ = admin.Get(srv.URL + "/admin/settings")
	page := body(t, resp)
	if !strings.Contains(page, `action="/admin/settings/mail-test"`) || !strings.Contains(page, `name="to"`) {
		t.Error("settings lack the test-mail form with a recipient field")
	}
	if !strings.Contains(page, "письмом на wishlist@example.com") || strings.Contains(page, "Telegram") {
		t.Error("settings do not say where owner notices go, or still mention Telegram")
	}
	// Nothing listens on the configured port: the error must name the problem, not claim success.
	resp, _ = admin.PostForm(srv.URL+"/admin/settings/mail-test", url.Values{"to": {"me@example.com"}})
	if page := body(t, resp); !strings.Contains(page, "Почта: ") || strings.Contains(page, "принял тестовое письмо") {
		t.Error("failed test mail not reported as a failure")
	}
	resp, _ = admin.PostForm(srv.URL+"/admin/settings/mail-test", url.Values{"to": {"not an address"}})
	if page := body(t, resp); !strings.Contains(page, "адрес получателя не похож на email") {
		t.Error("bad recipient not reported")
	}
}

func TestHeloName(t *testing.T) {
	if got := heloName(Config{BaseURL: "https://wishlist.stanislavski.me", SMTPFrom: "W <w@other.example>"}); got != "wishlist.stanislavski.me" {
		t.Errorf("from BASE_URL: %q", got)
	}
	if got := heloName(Config{SMTPFrom: "Wishlist <wishlist@stanislavski.me>"}); got != "stanislavski.me" {
		t.Errorf("from SMTP_FROM: %q", got)
	}
}

func TestMailTexts(t *testing.T) {
	_, app := newTestApp(t)
	app.cfg.BaseURL = "https://wishlist.example"
	exp := time.Date(2026, 11, 9, 12, 0, 0, 0, time.Local)
	for _, lang := range []string{"ru", "en"} {
		subj, body := app.confirmationMail(lang, "https://wishlist.example/r/tok", "Casio A140WE", exp)
		date := formatDate(lang, exp)
		for _, want := range []string{"Casio A140WE", "wishlist.example", date, "https://wishlist.example/r/tok"} {
			if !strings.Contains(body, want) {
				t.Errorf("%s confirmation lacks %q:\n%s", lang, want, body)
			}
		}
		if !strings.Contains(subj, "Casio A140WE") || strings.Contains(subj+body, "%!") {
			t.Errorf("%s confirmation subject/body broken: %q\n%s", lang, subj, body)
		}
		for _, alarm := range []string{"Важно", "Important"} {
			if strings.Contains(body, alarm) {
				t.Errorf("%s confirmation still says %q", lang, alarm)
			}
		}
		rs := tf(lang, "mail_reminder_subject", "Casio A140WE")
		rb := tf(lang, "mail_reminder_body", "Casio A140WE", app.siteHost(), date, "https://wishlist.example/r/tok")
		if strings.Contains(rs+rb, "%!") || !strings.Contains(rb, date) || !strings.Contains(rb, "https://wishlist.example/r/tok") {
			t.Errorf("%s reminder broken: %q\n%s", lang, rs, rb)
		}
	}
}

func TestNotifyTo(t *testing.T) {
	if got := notifyTo(Config{SMTPFrom: "Wishlist <wishlist@stanislavski.me>"}); got != "wishlist@stanislavski.me" {
		t.Errorf("default = %q, want the sender address", got)
	}
	if got := notifyTo(Config{SMTPFrom: "Wishlist <wishlist@stanislavski.me>", NotifyEmail: "me@example.com"}); got != "me@example.com" {
		t.Errorf("NOTIFY_EMAIL ignored: %q", got)
	}
	if got := notifyTo(Config{}); got != "" {
		t.Errorf("no mail settings: %q", got)
	}
}
