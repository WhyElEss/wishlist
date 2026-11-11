package main

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// noisyPNG is a picture that compresses badly as PNG, like a product photo.
func noisyPNG(t *testing.T, w, h int, alpha bool) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	seed := uint32(1)
	for i := range img.Pix {
		seed = seed*1664525 + 1013904223
		img.Pix[i] = uint8(seed >> 24)
		if i%4 == 3 {
			img.Pix[i] = 255
			if alpha && i/4%w < w/2 {
				img.Pix[i] = 0 // left half transparent
			}
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestShrinkImage(t *testing.T) {
	in := noisyPNG(t, 1500, 1000, true)
	out, ext := shrinkImage(in, ".png")
	if ext != ".jpg" || len(out) >= len(in) {
		t.Fatalf("ext %s, %d -> %d bytes", ext, len(in), len(out))
	}
	img, err := jpeg.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != imageMaxSide || b.Dy() != 426 {
		t.Errorf("size %v, want 640x426", b)
	}
	// Transparent pixels become white, as on the cards.
	if r, g, b, _ := img.At(10, 200).RGBA(); r>>8 < 240 || g>>8 < 240 || b>>8 < 240 {
		t.Errorf("transparent area is %d,%d,%d, want white", r>>8, g>>8, b>>8)
	}

	// Already small: kept as it is.
	if out, ext := shrinkImage(pixel, ".png"); ext != ".png" || !bytes.Equal(out, pixel) {
		t.Errorf("tiny picture changed: %s %d bytes", ext, len(out))
	}
	// Not decodable (WebP): kept.
	webp := []byte("RIFF\x00\x00\x00\x00WEBPVP8 ")
	if out, ext := shrinkImage(webp, ".webp"); ext != ".webp" || !bytes.Equal(out, webp) {
		t.Error("undecodable picture changed")
	}
}

func TestDownscaleAverages(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 4, 2))
	for x := 0; x < 4; x++ {
		for y := 0; y < 2; y++ {
			c := uint8(0)
			if x%2 == 1 {
				c = 200
			}
			src.Set(x, y, color.RGBA{c, c, c, 255})
		}
	}
	d := downscale(src, 2)
	if d.Bounds().Dx() != 2 || d.Bounds().Dy() != 1 {
		t.Fatalf("size %v", d.Bounds())
	}
	if r, _, _, _ := d.At(0, 0).RGBA(); r>>8 != 100 {
		t.Errorf("average %d, want 100", r>>8)
	}
}

func TestWebpSize(t *testing.T) {
	riff := func(chunk string, data []byte) []byte {
		b := append([]byte("RIFF\x00\x00\x00\x00WEBP"+chunk+"\x00\x00\x00\x00"), data...)
		return append(b, make([]byte, 16)...)
	}
	// VP8: frame tag, start code, 14-bit width/height (1200x1050).
	lossy := riff("VP8 ", []byte{0, 0, 0, 0x9d, 0x01, 0x2a, 0xb0, 0x04, 0x1a, 0x04})
	// VP8L: signature, then width-1 and height-1 in 14 bits each (3000x3000).
	v := uint32(2999) | uint32(2999)<<14
	lossless := riff("VP8L", []byte{0x2f, byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24)})
	// VP8X: flags + reserved, then 24-bit width-1 and height-1 (640x480).
	ext := riff("VP8X", []byte{0, 0, 0, 0, 0x7f, 0x02, 0, 0xdf, 0x01, 0})
	for name, c := range map[string]struct {
		b    []byte
		w, h int
	}{"VP8": {lossy, 1200, 1050}, "VP8L": {lossless, 3000, 3000}, "VP8X": {ext, 640, 480}, "junk": {[]byte("nope"), 0, 0}} {
		if w, h := webpSize(c.b); w != c.w || h != c.h {
			t.Errorf("%s: %dx%d, want %dx%d", name, w, h, c.w, c.h)
		}
	}
}

// A CDN that sends WebP to browsers but PNG when asked for JPEG/PNG only.
func TestFetchImageAsksAgainForWebP(t *testing.T) {
	big := noisyPNG(t, 1000, 1000, false)
	webp := append([]byte("RIFF\x00\x00\x00\x00WEBPVP8 "), make([]byte, len(big))...)
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.Header.Get("Accept"), "webp") {
			w.Header().Set("Content-Type", "image/webp")
			w.Write(webp)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Write(big)
	}))
	defer cdn.Close()
	b, ext, err := fetchImage(context.Background(), cdn.Client(), cdn.URL+"/p", "")
	if err != nil || ext != ".jpg" || len(b) >= len(webp) {
		t.Fatalf("ext %s, %d bytes, err %v", ext, len(b), err)
	}
}

func TestShrinkStoredImages(t *testing.T) {
	_, app := newTestApp(t)
	big := noisyPNG(t, 1200, 1200, false)
	if err := os.WriteFile(filepath.Join(app.imagesDir, "abc-old.png"), big, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app.imagesDir, "def-tiny.png"), pixel, 0o644); err != nil {
		t.Fatal(err)
	}
	app.store.update(func(d *Data) error {
		d.Items = []*Item{{ID: "abc", Image: "abc-old.png", Status: statusActive, Quantity: 1, CreatedAt: time.Now()},
			{ID: "def", Image: "def-tiny.png", Status: statusActive, Quantity: 1, CreatedAt: time.Now()}}
		return nil
	})
	app.shrinkStoredImages(context.Background())
	var got, tiny string
	var done int
	app.store.view(func(d *Data) { got, tiny, done = d.Items[0].Image, d.Items[1].Image, d.ImagesShrunk })
	if !strings.HasPrefix(got, "abc-") || !strings.HasSuffix(got, ".jpg") || tiny != "def-tiny.png" || done != imagesShrunk {
		t.Fatalf("images %q %q, flag %d", got, tiny, done)
	}
	if _, err := os.Stat(filepath.Join(app.imagesDir, "abc-old.png")); !os.IsNotExist(err) {
		t.Error("old file kept")
	}
	if w, h := app.imageSize(got); w != imageMaxSide || h != imageMaxSide {
		t.Errorf("imageSize %dx%d", w, h)
	}
	// Runs once per data file.
	app.store.update(func(d *Data) error { d.Items[1].Image = "missing.png"; return nil })
	app.shrinkStoredImages(context.Background())
	app.store.view(func(d *Data) { tiny = d.Items[1].Image })
	if tiny != "missing.png" {
		t.Error("second run touched the store")
	}
}

func TestIndexingHeaders(t *testing.T) {
	srv, _ := newTestApp(t)
	for path, noindex := range map[string]bool{"/": false, "/admin/login": true, "/r/unknown": true} {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+path, nil)
		req.Header.Set("X-Forwarded-Proto", "https")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		page := body(t, resp)
		tagged := strings.Contains(page, `<meta name="robots" content="noindex`)
		header := resp.Header.Get("X-Robots-Tag") != ""
		if tagged != noindex || header != noindex {
			t.Errorf("%s: meta %v, header %v, want %v", path, tagged, header, noindex)
		}
		if resp.Header.Get("Strict-Transport-Security") != "max-age=31536000" || resp.Header.Get("Cross-Origin-Opener-Policy") != "same-origin" {
			t.Errorf("%s: HSTS %q, COOP %q", path, resp.Header.Get("Strict-Transport-Security"), resp.Header.Get("Cross-Origin-Opener-Policy"))
		}
	}
	resp, _ := http.Get(srv.URL + "/")
	resp.Body.Close()
	if resp.Header.Get("Strict-Transport-Security") != "" {
		t.Error("HSTS over plain HTTP")
	}
	resp, _ = http.Get(srv.URL + "/static/style.css?v=abc")
	resp.Body.Close()
	if cc := resp.Header.Get("Cache-Control"); !strings.Contains(cc, "max-age=31536000") {
		t.Errorf("versioned static: %q", cc)
	}
	resp, _ = http.Get(srv.URL + "/static/icon.svg")
	resp.Body.Close()
	if cc := resp.Header.Get("Cache-Control"); cc != "public, max-age=86400" {
		t.Errorf("unversioned static: %q", cc)
	}
}
