# Build stage
FROM  golang:1.21-alpine AS builder

# Install build dependencies
RUN apk add --no-cache gcc musl-dev

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the application (CGO required for sqlite3)
RUN CGO_ENABLED=1 go build -ldflags="-s -w" -o puter2api .

# Runtime stage
FROM alpine:latest

# Install runtime dependencies
RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/puter2api .

# Create data directory
RUN mkdir -p /data

# Environment variables
ENV PORT=8081
ENV DB_PATH=/data/puter2api.db
ENV GIN_MODE=release

# Expose port
EXPOSE 8081

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8081/ || exit 1

# Run the application
CMD ["./puter2api"]
