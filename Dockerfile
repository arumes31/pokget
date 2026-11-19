# Static asset stage
FROM node:26-alpine@sha256:6bd9ab43c0ec0190ca7b2d193906cb8b7edf6e7f4afa43ddeccd6452710bffb5 AS static-assets

WORKDIR /app

COPY package.json package-lock.json ./
RUN npm ci --ignore-scripts --no-audit --no-fund

COPY scripts ./scripts
COPY static ./static
COPY templates ./templates
RUN npm run build:static && npm run check:static

# Go build stage
FROM golang:1.27.4-alpine@sha256:70bd7ddea8b6cbe5d6943cde279d6b67feb10cc924fb9100d1d8757c1575b280 AS builder

# Pure-Go build: no CGO, no native OCR libraries.
ENV CGO_ENABLED=0

WORKDIR /app

# Copy go mod and sum files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code and templates/static assets
COPY . .

# Build the application
RUN go build -o main ./cmd/pokget && \
	go build -o catalog ./cmd/catalog

# Final stage
FROM alpine:3.24.4@sha256:f59abe656b692d901e184be3445e084786602108599d2a51d229c0f5a48c71bf

# Install runtime dependencies: Chromium for headless catalog scraping.
RUN apk add --no-cache \
    libcrypto3 \
    libssl3 \
    chromium \
    nss \
    freetype \
    harfbuzz \
    ca-certificates \
    ttf-freefont

RUN addgroup -S -g 10001 pokget \
    && adduser -S -D -H -u 10001 -G pokget pokget \
    && mkdir -p /app/data/cache /app/data/catalog-images /app/data/card-photos /tmp/pokget \
    && chown -R pokget:pokget /app /tmp/pokget

ENV HOME=/tmp/pokget

WORKDIR /app

# Copy binary from builder
COPY --chown=pokget:pokget --from=builder /app/main .
COPY --chown=pokget:pokget --from=builder /app/catalog .
COPY --chown=pokget:pokget --from=builder /app/templates ./templates
COPY --chown=pokget:pokget --from=static-assets /app/dist/static ./static
COPY --chown=pokget:pokget --from=builder /app/migrations ./migrations

# Expose port
EXPOSE 18066

HEALTHCHECK --interval=30s --timeout=15s --start-period=90s --retries=3 \
  CMD ["sh", "-c", "wget -q --spider http://127.0.0.1:${APP_PORT:-18066}/health/ready"]

USER pokget

# Run the binary
CMD ["./main"]
