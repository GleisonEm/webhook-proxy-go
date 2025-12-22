# Build Stage
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Install dependencies for build
# git might be needed for go mod download if private repos (safe to keep)
# ca-certificates needed to copy to final stage
RUN apk add --no-cache git ca-certificates tzdata

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY . .

# Build binary
# -ldflags="-s -w" strips debug information -> smaller binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o server ./cmd/server

# Runtime Stage - SCATCH (Empty image, smallest possible)
FROM scratch

WORKDIR /app

# Copy CA certificates for HTTPS
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Copy Timezone data (optional but good for logs)
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo

# Copy binary from builder
COPY --from=builder /app/server .

# Expose port (application default)
EXPOSE 8095

# Run
CMD ["./server"]
