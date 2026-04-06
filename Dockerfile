# ─────────────────────────────────────────────────────────────────────────────
# Stage 1: Build the Next.js admin UI (standalone output)
# ─────────────────────────────────────────────────────────────────────────────
FROM node:24-alpine AS ui-builder

RUN apk add --no-cache python3 make g++

WORKDIR /app/dashboard
COPY dashboard/package.json dashboard/pnpm-lock.yaml ./
RUN corepack enable pnpm && pnpm install --frozen-lockfile

COPY dashboard/ .

# Env vars baked into the JS bundle at build time by Next.js
ARG NEXT_PUBLIC_ENABLE_FILE_STORAGE=false
ENV NEXT_PUBLIC_ENABLE_FILE_STORAGE=$NEXT_PUBLIC_ENABLE_FILE_STORAGE

RUN pnpm build

# ─────────────────────────────────────────────────────────────────────────────
# Stage 2: Build the Go server binary
# ─────────────────────────────────────────────────────────────────────────────
FROM golang:1.24-alpine AS go-builder

RUN apk add --no-cache gcc musl-dev

WORKDIR /app/server
COPY server/go.mod server/go.sum ./
RUN go mod download

COPY server/ .
RUN CGO_ENABLED=1 GOOS=linux go build -o sqlite-hub-server ./cmd/server

# ─────────────────────────────────────────────────────────────────────────────
# Development stage — hot-reload via Next.js dev server
# Must come before the production stage so that `docker build` (and Railway)
# targets `production` by default (last stage wins).
# ─────────────────────────────────────────────────────────────────────────────
FROM caddy:2-alpine AS development

RUN apk add --no-cache \
    nodejs \
    npm \
    supervisor \
    curl \
    python3 \
    make \
    g++

WORKDIR /app
RUN mkdir -p /data/files/blobs

# ── Go binary ────────────────────────────────────────────────────────────────
COPY --from=go-builder /app/server/sqlite-hub-server ./server/sqlite-hub-server

COPY dashboard/package.json dashboard/pnpm-lock.yaml ./dashboard/
RUN npm install -g pnpm && cd dashboard && pnpm install

COPY dashboard/ ./dashboard/
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

# Runtime deps: Node.js for Next.js standalone, supervisord, curl for health checks.
RUN apk add --no-cache \
    nodejs \
    supervisor \
    curl

WORKDIR /app

# Create data & blob directories
RUN mkdir -p /data/files/blobs

# ── Next.js standalone build ──────────────────────────────────────────────────
COPY --from=ui-builder /app/dashboard/.next/standalone ./dashboard/.next/standalone
COPY --from=ui-builder /app/dashboard/.next/static ./dashboard/.next/standalone/.next/static
COPY --from=ui-builder /app/dashboard/public ./dashboard/.next/standalone/public

# ── Go binary ────────────────────────────────────────────────────────────────
COPY --from=go-builder /app/server/sqlite-hub-server ./server/sqlite-hub-server

# ── supervisord config ───────────────────────────────────────────────────────
COPY supervisord.conf /etc/supervisor/conf.d/sqlite-hub.conf

# ── Caddy startup script (generates Caddyfile dynamically) ──────────────────
COPY start.sh /app/start.sh
RUN chmod +x /app/start.sh

EXPOSE 80
EXPOSE 443

CMD ["/app/start.sh"]
