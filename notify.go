package main

import (
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"net/url"
	"strings"
	"time"
)

// helo is the name given to the SMTP server in EHLO; without it Go says
// "localhost", which shows up in the Received header spam filters read.
type mailer struct {
	host, port, user, pass, from, helo string
}

func (m *mailer) enabled() bool { return m.host != "" && m.from != "" }

// send submits one message over SMTP with STARTTLS (port 587). It refuses
// to authenticate on a connection the server did not encrypt.
func (m *mailer) send(to, subject, body string) error {
	if !m.enabled() {
		return nil
	}
	from, err := mail.ParseAddress(m.from)
	if err != nil {
		return fmt.Errorf("SMTP_FROM: %w", err)
	}
	rcpt, err := mail.ParseAddress(to)
	if err != nil {
		return err
	}
	_, domain, _ := strings.Cut(from.Address, "@")
	msg := "From: " + from.String() + "\r\n" +
		"To: " + rcpt.Address + "\r\n" +
		"Subject: " + mime.QEncoding.Encode("utf-8", subject) + "\r\n" +
		"Date: " + time.Now().Format(time.RFC1123Z) + "\r\n" +
		"Message-ID: <" + randomString(18) + "@" + domain + ">\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"Content-Transfer-Encoding: 8bit\r\n\r\n" +
		strings.ReplaceAll(body, "\n", "\r\n")

	conn, err := net.DialTimeout("tcp", net.JoinHostPort(m.host, m.port), 20*time.Second)
	if err != nil {
		return err
	}
	conn.SetDeadline(time.Now().Add(60 * time.Second))
	c, err := smtp.NewClient(conn, m.host)
	if err != nil {
		conn.Close()
		return err
	}
	defer c.Close()
	if m.helo != "" {
		if err := c.Hello(m.helo); err != nil {
			return err
		}
	}
	if ok, _ := c.Extension("STARTTLS"); !ok {
		return errors.New("SMTP server does not offer STARTTLS")
	}
	if err := c.StartTLS(&tls.Config{ServerName: m.host}); err != nil {
		return err
	}
	if m.user != "" {
		if err := c.Auth(smtp.PlainAuth("", m.user, m.pass, m.host)); err != nil {
			return err
		}
	}
	if err := c.Mail(from.Address); err != nil {
		return err
	}
	if err := c.Rcpt(rcpt.Address); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write([]byte(msg)); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

// heloName is the public host name of the site (wishlist.stanislavski.me), or
// the sender's domain when BASE_URL is not set.
func heloName(cfg Config) string {
	if u, err := url.Parse(cfg.BaseURL); err == nil && u.Hostname() != "" {
		return u.Hostname()
	}
	if a, err := mail.ParseAddress(cfg.SMTPFrom); err == nil {
		if _, domain, ok := strings.Cut(a.Address, "@"); ok {
			return domain
		}
	}
	return ""
}

// notifyTo is where the owner's notices go: NOTIFY_EMAIL, or the sending
// address itself (wishlist@…) when that is not set.
func notifyTo(cfg Config) string {
	if cfg.NotifyEmail != "" {
		return cfg.NotifyEmail
	}
	if a, err := mail.ParseAddress(cfg.SMTPFrom); err == nil {
		return a.Address
	}
	return ""
}

// notify emails the owner about a reservation change, in the background;
// failures only reach the log. Without mail settings it does nothing.
func (a *App) notify(subject, text string) {
	to := notifyTo(a.cfg)
	if !a.mail.enabled() || to == "" {
		return
	}
	body := text + "\n\n" + a.cfg.BaseURL + "/admin\n"
	go func() {
		if err := a.mail.send(to, "Вишлист: "+subject, body); err != nil {
			log.Printf("owner notice: %v", err)
		}
	}()
}
