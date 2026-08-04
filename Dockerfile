# Static asset stage
FROM node:24-alpine AS static-assets

WORKDIR /app

COPY package.json package-lock.json ./
RUN npm ci --ignore-scripts --no-audit --no-fund

COPY scripts ./scripts
COPY static ./static
RUN npm run build:static && npm run check:static

# Go build stage
FROM golang:1.26.4-alpine AS builder

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
RUN go build -o main ./cmd/pokget/main.go && \
    go build -o catalog ./cmd/catalog/main.go

# Final stage
FROM alpine:latest

# Install runtime dependencies: Tesseract for OCR and Chromium for headless scraping
RUN apk add --no-cache \
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

ENV TESSDATA_PREFIX=/usr/share/tessdata

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/main .
COPY --from=builder /app/catalog .
COPY --from=builder /app/templates ./templates
COPY --from=static-assets /app/dist/static ./static
COPY --from=builder /app/migrations ./migrations

# Expose port
EXPOSE 18066

# Run the binary
CMD ["./main"]
