# Static asset stage
FROM node:26-alpine@sha256:2d984a15c9b54fd0aeb608b8e0d0d83529eb34d2966db27a1fb4f1edc3d298a3 AS static-assets

WORKDIR /app

COPY package.json package-lock.json ./
RUN npm ci --ignore-scripts --no-audit --no-fund

COPY scripts ./scripts
COPY static ./static
COPY templates ./templates
RUN npm run build:static && npm run check:static

# Go build stage
FROM golang:1.27.3-alpine AS builder

# Install Tesseract OCR dependencies
RUN apk add --no-cache \
    tesseract-ocr \
    tesseract-ocr-dev \
    gcc \
    g++ \
    musl-dev \
    build-base

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
FROM alpine:3.24.2@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6

# Install runtime dependencies: Tesseract for OCR and Chromium for headless scraping
RUN apk add --no-cache \
    libcrypto3=3.5.9-r0 \
    libssl3=3.5.9-r0 \
    tesseract-ocr \
    tesseract-ocr-data-eng \
    tesseract-ocr-data-jpn \
    tesseract-ocr-data-deu \
    tesseract-ocr-data-fra \
    tesseract-ocr-data-chi_sim \
    tesseract-ocr-data-chi_tra \
    tesseract-ocr-data-kor \
    chromium \
    nss \
    freetype \
    harfbuzz \
    ca-certificates \
    ttf-freefont

RUN addgroup -S -g 10001 pokget \
    && adduser -S -D -H -u 10001 -G pokget pokget \
    && mkdir -p /app/data/cache /app/data/catalog-images /tmp/pokget \
    && chown -R pokget:pokget /app /tmp/pokget

ENV TESSDATA_PREFIX=/usr/share/tessdata \
    HOME=/tmp/pokget

WORKDIR /app

# Copy binary from builder
COPY --chown=pokget:pokget --from=builder /app/main .
COPY --chown=pokget:pokget --from=builder /app/catalog .
COPY --chown=pokget:pokget --from=builder /app/templates ./templates
COPY --chown=pokget:pokget --from=static-assets /app/dist/static ./static
COPY --chown=pokget:pokget --from=builder /app/migrations ./migrations

# Expose port
EXPOSE 18066

HEALTHCHECK --interval=30s --timeout=3s --start-period=20s --retries=3 \
  CMD ["sh", "-c", "wget -q --spider http://127.0.0.1:${APP_PORT:-18066}/health/ready"]

USER pokget

# Run the binary
CMD ["./main"]
