package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"net/url"
	"strings"
	"time"
)

type telegram struct {
	token, chat string
	client      *http.Client
}

func (t *telegram) enabled() bool { return t.token != "" && t.chat != "" }

// send delivers in the background; failures only reach the log.
func (t *telegram) send(text string) {
	if !t.enabled() {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := t.sendSync(ctx, text); err != nil {
			log.Printf("telegram: %v", err)
		}
	}()
}

func (t *telegram) sendSync(ctx context.Context, text string) error {
	if !t.enabled() {
		return errors.New("TELEGRAM_BOT_TOKEN or TELEGRAM_CHAT_ID is not set")
	}
	form := url.Values{"chat_id": {t.chat}, "text": {text}, "disable_web_page_preview": {"true"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.telegram.org/bot"+t.token+"/sendMessage", strings.NewReader(form.Encode()))
	if err != nil {
		return t.redact(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := t.client.Do(req)
	if err != nil {
		return t.redact(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}

// redact keeps the bot token out of logs: url.Error includes the request URL.
func (t *telegram) redact(err error) error {
	return errors.New(strings.ReplaceAll(err.Error(), t.token, "***"))
}

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
