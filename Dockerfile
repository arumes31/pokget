# Build stage
FROM golang:1.26-alpine AS builder

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
RUN go build -o main ./cmd/pokget/main.go

# Final stage
FROM alpine:latest

# Install runtime dependencies: Tesseract for OCR and Chromium for headless scraping
RUN apk add --no-cache \
    tesseract-ocr \
    chromium \
    nss \
    freetype \
    harfbuzz \
    ca-certificates \
    ttf-freefont

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/main .
COPY --from=builder /app/templates ./templates
COPY --from=builder /app/static ./static
COPY --from=builder /app/migrations ./migrations

# Expose port
EXPOSE 8080

# Run the binary
CMD ["./main"]
