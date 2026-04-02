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
# Stage 2: Build the Next.js UI
# ─────────────────────────────────────────────────────────────────────────────
FROM node:24-alpine AS nextjs-builder

RUN apk add --no-cache python3 make g++

WORKDIR /app
COPY package.json pnpm-lock.yaml ./
RUN corepack enable pnpm && pnpm install --frozen-lockfile

COPY . .
RUN pnpm build

# ─────────────────────────────────────────────────────────────────────────────
# Stage 3: Production image — Caddy + supervisord + Go binary + Next.js standalone
# ─────────────────────────────────────────────────────────────────────────────
FROM caddy:2-alpine AS production

# Runtime deps: supervisord, curl for health checks, libc / libgcc for CGO binary.
# Node.js is copied from the builder stage so that the V8 ABI exactly matches
# what better-sqlite3 was compiled against (Alpine's apk nodejs lacks V8 symbols).
RUN apk add --no-cache \
    supervisor \
    curl \
    libgcc \
    libstdc++ \
    libc6-compat

# Copy the exact Node.js binary used to build the standalone app
COPY --from=nextjs-builder /usr/local/bin/node /usr/local/bin/node

WORKDIR /app

# Create data & blob directories
RUN mkdir -p /data/files/blobs

# ── Go binary ────────────────────────────────────────────────────────────────
COPY --from=go-builder /build/sqlite-hub-server ./server/sqlite-hub-server

# ── Next.js standalone build ─────────────────────────────────────────────────
COPY --from=nextjs-builder /app/.next/standalone        ./nextjs/
COPY --from=nextjs-builder /app/public                  ./nextjs/public
COPY --from=nextjs-builder /app/.next/static            ./nextjs/.next/static

# ── supervisord config ───────────────────────────────────────────────────────
COPY supervisord.conf /etc/supervisor/conf.d/sqlite-hub.conf

# ── Caddy startup script (generates Caddyfile dynamically) ──────────────────
COPY start.sh /app/start.sh
RUN chmod +x /app/start.sh

EXPOSE 80
EXPOSE 443

CMD ["/app/start.sh"]

# ─────────────────────────────────────────────────────────────────────────────
# Development stage — hot-reload for both Go (air) and Next.js
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
