# wishlist

A self-hosted gift wishlist with anonymous reservations. One Go binary built only on the standard library, all state in one JSON file, runs in Docker on a Raspberry Pi behind a Cloudflare Tunnel.

The owner adds links to any online shop; guests see cards with a picture, price and store, pick a gift without signing up, and the card turns grey for everyone else. After reserving, the guest gets the shipping address to use and a private link to mark the gift as bought or cancel.

The guest-facing site is in Russian and English (picked from the browser, with a switch). The admin panel is in Russian.

## Features

**For guests**

- A tap anywhere on a card opens the next step; shop links appear only after that.
- Anonymous reservation: no name or account. The reservation lasts N days (14 by default) unless marked as bought.
- After reserving: the shipping address as copyable fields (tap a field to copy it, or **Copy All**), the store link, **I bought it** and **Cancel**.
- Shipping rules shown before reserving: hand it over in person and order to your own address, or ship to the owner. A US store ships to the US address, a store in any other country to the Canadian one. If the gift is tied to one country, only that address is offered.
- Optional email: a link to the reservation right away and one reminder 3 days before it expires.
- The browser remembers its own reservations (cookie), so the card says "You're gifting this".

**For the owner (admin panel)**

- Add a gift by pasting a link; title, picture and price are read from the page.
- Reorder gifts by dragging rows (mouse, touch or arrow keys). Gifts marked ★ **Really want** float above the manual order and get a red frame; reserved gifts go to the end.
- Per-gift quantity, notes, status (active / received / already have) and shipping region (auto / US only / Canada only).
- Spoiler-free mode (default): the admin panel and the owner's notices do not reveal what was reserved.
- Prices are re-read daily at 06:00 (container time zone), and one minute after start when the last check is more than a day old. A price is only replaced by a price actually read from the shop; manual prices for shops that cannot be read stay as they are.
- Email notices to the owner about reservations, purchases, cancellations and expiries.
- Shipping addresses for the US and Canada are edited in the settings as separate fields (name, two address lines, city, province or state, postal code, country). They are stored in the data volume, not in the repository.

**Site**

- `noindex` for search engines; link-preview crawlers (Twitter, Facebook, Telegram, Bluesky and others) may read the public pages, so shared links get a card.
- Open Graph card (1200×630, English), favicon, and an `apple-touch-icon` for Safari Favorites and the iOS home screen.
- Rate limits on reservations (5 per hour per IP) and admin logins (5 per 15 minutes per IP).

## How shops are read

| Shop | How | Notes |
|---|---|---|
| amazon.com | Product page in a cookie session with a US delivery ZIP | Seen from Canada, amazon.com hides prices of items that do not ship there. The app sets the ZIP through Amazon's own "Deliver to" dialog once per session. The ZIP is the postal code of the US address in the settings (10001 if empty). |
| amazon.ca | Product page, browser User-Agent | Price in CAD. |
| Discogs | Public API at `api.discogs.com` (no key) | Pages are behind a Cloudflare challenge. `/master/…` and `/release/…` and `/sell/release/…` show the cheapest marketplace offer in USD as "from $X"; `/shop/item/…` and `/sell/item/…` show the listing's fixed price. |
| Any other shop | OpenGraph tags and schema.org `Product` JSON-LD | Tried with a browser User-Agent and with the Facebook preview crawler's. |
| Sites that refuse non-browser clients | Not readable | Seen so far: eBay and Casio (Akamai Bot Manager), Best Buy Canada and LEGO (HTTP 403). The card is created with a note; title, price and picture are filled in by hand. |

Images are downloaded once and served from the app, so cards do not depend on the shop's image links.

## Architecture

```
 guests ──https──▶ Cloudflare ──tunnel──▶ cloudflared ──http://wishlist:8095──▶ wishlist (Go)
                                                                                    │
                                     shops, api.discogs.com ◀───────────────────────┤
                                     smtp.resend.com:587 (STARTTLS) ◀───────────────┤
                                                                                    ▼
                                                              /data: wishlist.json, session.key, images/
```

| File | What it does |
|---|---|
| `main.go` | Start-up, graceful shutdown, `hashpw` sub-command |
| `config.go` | Environment variables |
| `app.go` | Routes, security headers and same-origin check for POSTs, rendering, `robots.txt`, reservation expiry sweep |
| `store.go` | Data model and the JSON store: one mutex, atomic file replace; settings and addresses |
| `handlers_public.go` | List, reserve, reservation page, bought, cancel, language switch, images |
| `handlers_admin.go` | Login, gifts, ordering, refresh from the shop, settings, test email |
| `fetch.go` | Page fetching and OpenGraph / JSON-LD parsing, image download, region detection |
| `amazon.go` | amazon.com session with a US ZIP; amazon.ca; shop dispatch |
| `discogs.go` | Discogs API reader |
| `prices.go` | Daily price check |
| `notify.go` | SMTP client (STARTTLS only), owner notices |
| `auth.go` | PBKDF2 password hash, HMAC-signed admin session cookie |
| `ratelimit.go` | Per-IP limits, client IP and country from Cloudflare headers |
| `i18n.go` | Russian and English texts, price and date formatting |
| `templates/`, `static/` | HTML templates, CSS, a small script for copying, tabs and drag ordering; embedded into the binary |

Data lives in `wishlist.json` in the data volume. The gift list is in display order; reservations sit inside each gift with their secret token, optional email and expiry. A reservation is made under the store lock, so two guests can never take the same last unit.

## Running on a server

The host keeps only `docker-compose.yml` and `.env`; Compose builds the image straight from this repository.

| | |
|---|---|
| Compose project / container | `wishlist` |
| Network | `wishlist_net` |
| Volume | `wishlist_data` (`wishlist.json`, `session.key`, `images/`) |
| Port | `8095` |

```bash
mkdir -p ~/wishlist && cd ~/wishlist
curl -fsSLO https://raw.githubusercontent.com/WhyElEss/wishlist/main/docker-compose.yml
curl -fsSL -o .env https://raw.githubusercontent.com/WhyElEss/wishlist/main/.env.example
chmod 600 .env
docker compose build
docker compose run --rm -it wishlist hashpw
```

Put the printed `ADMIN_PASSWORD_HASH=...` line into `.env` (the input is hidden while typing), then:

```bash
docker compose up -d
```

The site is at `http://<host>:8095`, the admin panel at `/admin`.

Update:

```bash
cd ~/wishlist && docker compose build --no-cache && docker compose up -d
```

Backup of the data volume:

```bash
docker run --rm -v wishlist_data:/data -v "$PWD":/backup alpine tar czf /backup/wishlist-data.tgz -C /data .
```

### Public hostname (Cloudflare Tunnel)

The tunnel runs as a separate Compose project, `~/cloudflared-<name>`, with `TUNNEL_TOKEN` in its own `.env` (mode 600). It joins `wishlist_net` and reaches the app as `http://wishlist:8095`, so the host port is not needed for it:

```yaml
services:
  cloudflared:
    image: cloudflare/cloudflared:latest
    container_name: cloudflared-<name>
    restart: always
    command: tunnel --no-autoupdate run
    environment:
      TUNNEL_TOKEN: ${TUNNEL_TOKEN}
    networks:
      - wishlist_net

networks:
  wishlist_net:
    external: true
```

In Cloudflare: **Networking → Tunnels → Create a tunnel**, copy the token, then **Routes → Add route → Published application** with the service URL `http://wishlist:8095`. Set `BASE_URL` in the app's `.env` to the public origin.

### Email

Guest emails and the owner's notices go out over SMTP with STARTTLS; without `SMTP_HOST` the email field is hidden from guests and no mail is sent. Use a service made for application mail, such as [Resend](https://resend.com): verify the domain there, add the DNS records it gives, and create a sending-only API key.

```
SMTP_HOST=smtp.resend.com
SMTP_PORT=587
SMTP_USER=resend
SMTP_PASSWORD=<API key>
SMTP_FROM=Wishlist <wishlist@example.com>
```

A personal iCloud Mail account is not a good fit: messages sent through `smtp.mail.me.com` from an app reached Apple addresses but were rejected for Gmail with `550 5.7.1 [CS101] Message rejected due to local policy`.

**Settings → Send test email** sends the real reservation email (with a sample gift) to an address you type in.

## Configuration

See [.env.example](.env.example).

| Variable | Meaning |
|---|---|
| `ADMIN_PASSWORD_HASH` | Required. Output of `wishlist hashpw` (PBKDF2-SHA256, no `$` characters) |
| `BASE_URL` | Public origin, e.g. `https://wishlist.example.com`; used in email links and accepted as the Origin of form posts. Required when mail is on |
| `HOST_PORT` | Host port for the LAN (default 8095) |
| `TZ` | Time zone for dates and the 06:00 price check |
| `TRUST_PROXY_HEADERS` | Read the client IP and country from `CF-Connecting-IP` / `CF-IPCountry` (default `true`) |
| `SMTP_HOST`, `SMTP_PORT`, `SMTP_USER`, `SMTP_PASSWORD`, `SMTP_FROM` | Outgoing mail; see above |
| `NOTIFY_EMAIL` | Where the owner's notices go; empty means the `SMTP_FROM` address |

## Development

There are no dependencies beyond the Go standard library. CI runs `gofmt`, `go vet`, `go test -race` and a `linux/arm64` build on every push.

```bash
go test ./...
DATA_DIR=./data ADMIN_PASSWORD_HASH=... go run .
```

## License

No license has been chosen yet.
