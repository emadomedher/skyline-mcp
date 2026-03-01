# Build stage
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git

# Copy dependency files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build static binary
RUN CGO_ENABLED=0 go build \
    -ldflags "-X main.Version=$(cat VERSION) -s -w" \
    -o /skyline ./cmd/skyline

# Runtime stage
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /skyline /usr/local/bin/skyline

# Create non-root user
RUN addgroup -S skyline && adduser -S skyline -G skyline
RUN mkdir -p /data/.skyline && chown -R skyline:skyline /data

USER skyline
WORKDIR /data
ENV HOME=/data

EXPOSE 8191

ENTRYPOINT ["skyline"]
CMD ["--bind=0.0.0.0:8191"]
