package main

import (
	"context"
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

type mailer struct {
	host, port, user, pass, from string
}

func (m *mailer) enabled() bool { return m.host != "" && m.from != "" }

// send uses SMTP with STARTTLS when the server offers it (port 587).
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
	var auth smtp.Auth
	if m.user != "" {
		auth = smtp.PlainAuth("", m.user, m.pass, m.host)
	}
	msg := "From: " + from.String() + "\r\n" +
		"To: " + rcpt.Address + "\r\n" +
		"Subject: " + mime.QEncoding.Encode("utf-8", subject) + "\r\n" +
		"Date: " + time.Now().Format(time.RFC1123Z) + "\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"Content-Transfer-Encoding: 8bit\r\n\r\n" +
		strings.ReplaceAll(body, "\n", "\r\n")
	return smtp.SendMail(net.JoinHostPort(m.host, m.port), auth, from.Address, []string{rcpt.Address}, []byte(msg))
}
