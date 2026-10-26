# Static Go binary on a small Alpine base. Builds natively for whatever
# platform Docker targets — linux/arm64 on the Raspberry Pi.

FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY *.go ./
COPY templates ./templates
COPY static ./static
ARG VERSION=docker
RUN go vet ./... && \
    CGO_ENABLED=0 go build -ldflags "-s -w -X main.version=${VERSION}" -trimpath -o /out/wishlist .

FROM alpine:3.21
# ca-certificates: TLS to shops and Telegram; tzdata: TZ= for dates and logs.
RUN apk add --no-cache ca-certificates tzdata
COPY --from=build /out/wishlist /usr/local/bin/wishlist

# Owned by 1000:1000 so a fresh named volume inherits a writable /data.
RUN mkdir -p /data && chown 1000:1000 /data
USER 1000:1000
ENV DATA_DIR=/data LISTEN_ADDR=:8095
VOLUME ["/data"]
EXPOSE 8095

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8095/healthz >/dev/null || exit 1

ENTRYPOINT ["/usr/local/bin/wishlist"]
