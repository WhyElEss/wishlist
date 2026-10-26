package main

import (
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// The hash format is pbkdf2-sha256:<iterations>:<salt>:<key>, base64url
// without padding. It has no "$", so compose never tries to interpolate it.
const (
	pbkdf2Iter    = 600000
	adminCookie   = "wl_admin"
	sessionMaxAge = 30 * 24 * time.Hour
)

var b64 = base64.RawURLEncoding

func hashPassword(pw string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, pw, salt, pbkdf2Iter, 32)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("pbkdf2-sha256:%d:%s:%s", pbkdf2Iter, b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

type pwHash struct {
	iter      int
	salt, key []byte
}

func parseHash(s string) (pwHash, error) {
	p := strings.Split(s, ":")
	if len(p) != 4 || p[0] != "pbkdf2-sha256" {
		return pwHash{}, errors.New("expected pbkdf2-sha256:<iter>:<salt>:<key>")
	}
	iter, err := strconv.Atoi(p[1])
	if err != nil || iter < 1 {
		return pwHash{}, errors.New("bad iteration count")
	}
	salt, err := b64.DecodeString(p[2])
	if err != nil || len(salt) == 0 {
		return pwHash{}, errors.New("bad salt")
	}
	key, err := b64.DecodeString(p[3])
	if err != nil || len(key) == 0 {
		return pwHash{}, errors.New("bad key")
	}
	return pwHash{iter: iter, salt: salt, key: key}, nil
}

func (h pwHash) verify(pw string) bool {
	k, err := pbkdf2.Key(sha256.New, pw, h.salt, h.iter, len(h.key))
	return err == nil && subtle.ConstantTimeCompare(k, h.key) == 1
}

// loadSessionKey returns the HMAC key for admin cookies, creating it on first
// run. Deleting the file logs every admin session out.
func loadSessionKey(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if err == nil && len(b) >= 32 {
		return b, nil
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	b = make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return b, os.WriteFile(path, b, 0o600)
}

func (a *App) signSession(exp time.Time) string {
	payload := strconv.FormatInt(exp.Unix(), 10)
	m := hmac.New(sha256.New, a.sessionKey)
	m.Write([]byte(payload))
	return payload + "." + b64.EncodeToString(m.Sum(nil))
}

func (a *App) isAdmin(r *http.Request) bool {
	c, err := r.Cookie(adminCookie)
	if err != nil {
		return false
	}
	payload, sig, ok := strings.Cut(c.Value, ".")
	if !ok {
		return false
	}
	exp, err := strconv.ParseInt(payload, 10, 64)
	if err != nil || time.Now().Unix() >= exp {
		return false
	}
	want, err := b64.DecodeString(sig)
	if err != nil {
		return false
	}
	m := hmac.New(sha256.New, a.sessionKey)
	m.Write([]byte(payload))
	return hmac.Equal(m.Sum(nil), want)
}

func (a *App) requireAdmin(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.isAdmin(r) {
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}
		h(w, r)
	}
}
