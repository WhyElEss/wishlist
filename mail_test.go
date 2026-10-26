package main

import (
	"bufio"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
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
	m := &mailer{host: host, port: port, user: "me@icloud.com", pass: "secret-app-password", from: "Wishlist <wishlist@example.com>"}
	err = m.send("guest@example.com", "s", "b")
	if err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Fatalf("send without STARTTLS: err = %v", err)
	}
	if seen := <-got; strings.Contains(seen, "AUTH") {
		t.Errorf("credentials sent without encryption: %s", seen)
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
	if page := body(t, resp); !strings.Contains(page, `action="/admin/settings/mail-test"`) || !strings.Contains(page, `name="to"`) {
		t.Error("settings lack the test-mail form with a recipient field")
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
