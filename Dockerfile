# ─────────────────────────────────────────────────────────────────────────────
# Stage 1: Build the Go service
# CGO is required for go-sqlite3.
# ─────────────────────────────────────────────────────────────────────────────
FROM golang:1.24-alpine AS go-builder

RUN apk add --no-cache gcc musl-dev

WORKDIR /build
COPY server/go.mod server/go.sum ./
RUN go mod download

COPY server/ ./
RUN CGO_ENABLED=1 GOOS=linux go build -ldflags="-s -w" -o sqlite-hub-server ./cmd/server

# ─────────────────────────────────────────────────────────────────────────────
# Stage 2: Build the Vite admin UI
# ─────────────────────────────────────────────────────────────────────────────
FROM node:24-alpine AS ui-builder

RUN apk add --no-cache python3 make g++

WORKDIR /app
COPY package.json pnpm-lock.yaml ./
RUN corepack enable pnpm && pnpm install --frozen-lockfile

COPY . .

# Env vars baked into the JS bundle at build time by Vite
ARG NEXT_PUBLIC_ENABLE_FILE_STORAGE=false
ENV NEXT_PUBLIC_ENABLE_FILE_STORAGE=$NEXT_PUBLIC_ENABLE_FILE_STORAGE

RUN pnpm build

# ─────────────────────────────────────────────────────────────────────────────
# Development stage — hot-reload for both Go (air) and Vite
# Must come before the production stage so that `docker build` (and Railway)
# targets `production` by default (last stage wins).
# ─────────────────────────────────────────────────────────────────────────────
FROM caddy:2-alpine AS development

RUN apk add --no-cache \
    nodejs \
    npm \
    go \
    gcc \
    musl-dev \
    supervisor \
    curl \
    libgcc \
    libc6-compat \
    python3 \
    make \
    g++

# Install air for Go hot-reload
RUN go install github.com/air-verse/air@latest

WORKDIR /app
RUN mkdir -p /data/files/blobs

COPY package.json pnpm-lock.yaml ./
RUN corepack enable pnpm && pnpm install

COPY . .
COPY start.sh /app/start.sh
RUN chmod +x /app/start.sh

EXPOSE 80
EXPOSE 443

CMD ["/app/start.sh"]

# ─────────────────────────────────────────────────────────────────────────────
# Stage 3: Production image — Caddy + supervisord + Go binary + static Vite dist
# This is the last stage — Docker and Railway build this target by default.
# ─────────────────────────────────────────────────────────────────────────────
FROM caddy:2-alpine AS production

# Runtime deps: supervisord, curl for health checks, libc / libgcc for CGO binary.
RUN apk add --no-cache \
    supervisor \
    curl \
    libgcc \
    libstdc++ \
    libc6-compat

WORKDIR /app

# Create data & blob directories
RUN mkdir -p /data/files/blobs

# ── Go binary ────────────────────────────────────────────────────────────────
COPY --from=go-builder /build/sqlite-hub-server ./server/sqlite-hub-server

# ── Vite static build ────────────────────────────────────────────────────────
COPY --from=ui-builder /app/dist ./dist

# ── supervisord config ───────────────────────────────────────────────────────
COPY supervisord.conf /etc/supervisor/conf.d/sqlite-hub.conf

# ── Caddy startup script (generates Caddyfile dynamically) ──────────────────
COPY start.sh /app/start.sh
RUN chmod +x /app/start.sh

EXPOSE 80
EXPOSE 443

CMD ["/app/start.sh"]
