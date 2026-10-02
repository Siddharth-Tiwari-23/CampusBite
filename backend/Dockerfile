# ==========================================
# Stage 1: Build Stage
# ==========================================
FROM golang:1.24-alpine3.21 AS builder

WORKDIR /app

ENV GOTOOLCHAIN=auto

# Install git and ca-certificates for fetching dependencies
RUN apk add --no-cache git ca-certificates tzdata

# Leverage Docker layer caching for dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy backend source code
COPY . .

# Build production binaries with stripped debug symbols (-s -w) for small footprint
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /bin/server ./cmd/server && \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /bin/migrate ./cmd/migrate && \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /bin/seed ./cmd/seed

# ==========================================
# Stage 2: Minimal Runtime Stage
# ==========================================
FROM alpine:3.21 AS runner

# Install essential certificates for secure HTTPS API calls (Gemini, Razorpay) and timezone data
RUN apk --no-cache add ca-certificates tzdata

# Create unprivileged application user
RUN addgroup -g 10001 -S campusbite && \
    adduser -u 10001 -S campusbite -G campusbite

WORKDIR /app

# Copy compiled binaries from builder stage
COPY --from=builder /bin/server /app/server
COPY --from=builder /bin/migrate /app/migrate
COPY --from=builder /bin/seed /app/seed

# Copy SQL migrations for container runtime migration capability
COPY --from=builder /app/migrations /app/migrations

# Set ownership to unprivileged user
RUN chown -R campusbite:campusbite /app

USER campusbite

# Default port
ENV PORT=8080

EXPOSE 8080

# Default entrypoint runs the main server
ENTRYPOINT ["/app/server"]
