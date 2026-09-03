# ---- Build stage ----
FROM golang:1.26-alpine AS builder

WORKDIR /app

# Cache deps first
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY . .

# Build a static binary (CGO disabled so it runs on scratch/alpine with no libc issues)
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/server .

# ---- Runtime stage ----
FROM alpine:3.20

# certs in case your app makes outbound HTTPS calls, tzdata if you use time zones
RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

# Binary
COPY --from=builder /app/server .

# App assets
COPY static/ ./static/
COPY templates/ ./templates/
COPY assets/ ./assets/

# Mountpoint for the db directory (bind-mount or named volume at runtime)
RUN mkdir -p /app/db


ENTRYPOINT ["./server"]
CMD ["-port", "5003"]