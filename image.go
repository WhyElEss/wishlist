package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif" // decoders for image.Decode
	"image/jpeg"
	_ "image/png"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Pictures are stored at most imageMaxSide pixels on their longest side. The
// largest picture on the site is a card on the list, about 300 CSS pixels
// wide; 640 covers that on 2× screens. Quality 75 was chosen after encoding
// every stored picture at 70–90 (2026-11-11). imageMaxPixels guards against
// decompression bombs: an 8 MB PNG can claim a huge canvas.
const (
	imageMaxSide   = 640
	imageQuality   = 75
	imageMaxPixels = 25_000_000
)

// shrinkImage re-encodes a JPEG, PNG or GIF as a JPEG on a white background
// (cards and the reserve step show pictures on white), scaled down to
// imageMaxSide. It returns the input unchanged when it cannot decode it or
// when the result would not be smaller.
func shrinkImage(b []byte, ext string) ([]byte, string) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > imageMaxPixels {
		return b, ext
	}
	src, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		return b, ext
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, downscale(src, imageMaxSide), &jpeg.Options{Quality: imageQuality}); err != nil {
		return b, ext
	}
	if buf.Len() >= len(b) {
		return b, ext
	}
	return buf.Bytes(), ".jpg"
}

// downscale flattens src onto white and shrinks it so its longest side is at
// most max, averaging each block of source pixels (area averaging).
func downscale(src image.Image, max int) *image.RGBA {
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()
	flat := image.NewRGBA(image.Rect(0, 0, sw, sh))
	draw.Draw(flat, flat.Bounds(), &image.Uniform{color.White}, image.Point{}, draw.Src)
	draw.Draw(flat, flat.Bounds(), src, sb.Min, draw.Over)

	dw, dh := sw, sh
	if sw >= sh && sw > max {
		dw, dh = max, sh*max/sw
	} else if sh > sw && sh > max {
		dw, dh = sw*max/sh, max
	}
	dw, dh = maxInt(dw, 1), maxInt(dh, 1)
	if dw == sw && dh == sh {
		return flat
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < dh; y++ {
		y0, y1 := y*sh/dh, (y+1)*sh/dh
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for x := 0; x < dw; x++ {
			x0, x1 := x*sw/dw, (x+1)*sw/dw
			if x1 <= x0 {
				x1 = x0 + 1
			}
			var r, g, b, n uint32
			for yy := y0; yy < y1; yy++ {
				row := flat.Pix[yy*flat.Stride:]
				for xx := x0; xx < x1; xx++ {
					p := row[xx*4:]
					r += uint32(p[0])
					g += uint32(p[1])
					b += uint32(p[2])
					n++
				}
			}
			o := dst.PixOffset(x, y)
			dst.Pix[o], dst.Pix[o+1], dst.Pix[o+2], dst.Pix[o+3] = uint8(r/n), uint8(g/n), uint8(b/n), 255
		}
	}
	return dst
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// undecodable are formats the standard library cannot read, so they cannot
// be shrunk.
func undecodable(ext string) bool { return ext == ".webp" || ext == ".avif" }

// fetchImage downloads a picture and shrinks it. Many shop CDNs send WebP or
// AVIF to clients that accept them; for those it asks once more for JPEG or
// PNG only and keeps that answer when it is one of them (measured
// 2026-11-11: shop.yello.com and casio.com answer with PNG, CloudFront's
// converted .webp files stay WebP).
func fetchImage(ctx context.Context, c *http.Client, src, referer string) ([]byte, string, error) {
	b, ext, err := downloadImage(ctx, c, src, referer, imageAccept)
	if err != nil {
		return nil, "", err
	}
	if undecodable(ext) {
		if b2, ext2, err := downloadImage(ctx, c, src, referer, "image/jpeg,image/png;q=0.9"); err == nil && !undecodable(ext2) {
			if s, sext := shrinkImage(b2, ext2); len(s) < len(b) {
				return s, sext, nil
			}
		}
		return b, ext, nil
	}
	s, sext := shrinkImage(b, ext)
	return s, sext, nil
}

// imagesShrunk is the Data.ImagesShrunk value once pictures stored before
// shrinking existed have been shrunk.
const imagesShrunk = 1

// shrinkStoredImages shrinks pictures downloaded before fetchImage shrank
// them, once per data file: each is re-encoded from the stored file, or, for
// WebP and AVIF, downloaded again from its source. A shrunk picture gets a
// new file name, so browsers holding the old one for a year load the new one.
func (a *App) shrinkStoredImages(ctx context.Context) {
	type job struct{ id, image, src, page string }
	var jobs []job
	done := false
	a.store.view(func(d *Data) {
		if d.ImagesShrunk >= imagesShrunk {
			done = true
			return
		}
		for _, it := range d.Items {
			if it.Image != "" {
				jobs = append(jobs, job{it.ID, it.Image, it.ImageSrc, it.URL})
			}
		}
	})
	if done {
		return
	}
	var before, after, changed int
	for _, j := range jobs {
		if ctx.Err() != nil {
			return
		}
		old, err := os.ReadFile(filepath.Join(a.imagesDir, j.image))
		if err != nil {
			log.Printf("shrink image %s: %v", j.image, err)
			continue
		}
		ext := filepath.Ext(j.image)
		b, next := shrinkImage(old, ext)
		if undecodable(ext) && j.src != "" {
			rctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			if nb, e, err := fetchImage(rctx, a.client, j.src, j.page); err != nil {
				log.Printf("shrink image %s: download: %v", j.image, err)
			} else if len(nb) < len(old) {
				b, next = nb, e
			}
			cancel()
		}
		before += len(old)
		if len(b) >= len(old) {
			after += len(old)
			continue
		}
		name := j.id + "-" + newID() + next
		if err := os.WriteFile(filepath.Join(a.imagesDir, name), b, 0o644); err != nil {
			log.Printf("shrink image %s: %v", j.image, err)
			after += len(old)
			continue
		}
		swapped := false
		if err := a.store.update(func(d *Data) error {
			if it := d.item(j.id); it != nil && it.Image == j.image {
				it.Image, swapped = name, true
				return nil
			}
			return errNoChange
		}); err != nil {
			log.Printf("shrink image %s: %v", j.image, err)
		}
		if swapped {
			a.removeImage(j.image)
			after += len(b)
			changed++
		} else {
			a.removeImage(name) // the gift or its picture changed meanwhile
			after += len(old)
		}
	}
	if err := a.store.update(func(d *Data) error {
		d.ImagesShrunk = imagesShrunk
		return nil
	}); err != nil {
		log.Printf("shrink images: %v", err)
		return
	}
	if len(jobs) > 0 {
		log.Printf("shrink images: %d of %d pictures shrunk, %d KB -> %d KB", changed, len(jobs), before>>10, after>>10)
	}
}

// imageSize is a stored picture's width and height for the img tag, so the
// page keeps its layout while pictures load; 0, 0 when unknown. Stored file
// names never change content, so sizes are cached by name.
func (a *App) imageSize(name string) (int, int) {
	if v, ok := a.imageSizes.Load(name); ok {
		s := v.([2]int)
		return s[0], s[1]
	}
	var w, h int
	if reImageName.MatchString(name) {
		if f, err := os.Open(filepath.Join(a.imagesDir, name)); err == nil {
			head := make([]byte, 64<<10)
			n, _ := f.Read(head)
			f.Close()
			head = head[:n]
			if strings.HasSuffix(name, ".webp") {
				w, h = webpSize(head)
			} else if cfg, _, err := image.DecodeConfig(bytes.NewReader(head)); err == nil {
				w, h = cfg.Width, cfg.Height
			}
		}
	}
	a.imageSizes.Store(name, [2]int{w, h})
	return w, h
}

// webpSize reads the canvas size from a WebP header (RIFF container with a
// VP8, VP8L or VP8X chunk first; Google's "WebP Container Specification").
func webpSize(b []byte) (int, int) {
	if len(b) < 30 || string(b[0:4]) != "RIFF" || string(b[8:12]) != "WEBP" {
		return 0, 0
	}
	switch string(b[12:16]) {
	case "VP8 ":
		// Frame tag (3 bytes), start code 9d 01 2a, then 14-bit width and height.
		if b[23] != 0x9d || b[24] != 0x01 || b[25] != 0x2a {
			return 0, 0
		}
		return int(binary.LittleEndian.Uint16(b[26:]) & 0x3fff), int(binary.LittleEndian.Uint16(b[28:]) & 0x3fff)
	case "VP8L":
		if b[20] != 0x2f {
			return 0, 0
		}
		v := binary.LittleEndian.Uint32(b[21:])
		return int(v&0x3fff) + 1, int(v>>14&0x3fff) + 1
	case "VP8X":
		w := int(b[24]) | int(b[25])<<8 | int(b[26])<<16
		h := int(b[27]) | int(b[28])<<8 | int(b[29])<<16
		return w + 1, h + 1
	}
	return 0, 0
}
