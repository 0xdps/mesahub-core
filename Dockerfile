# Multi-stage build for efficient image size
FROM node:20-alpine AS builder

WORKDIR /app

# Install native build tools required by better-sqlite3
RUN apk add --no-cache python3 make g++

# Copy package files
COPY package*.json ./

# Install dependencies
RUN npm ci

# Copy source code
COPY . .

# Build Next.js
RUN npm run build

# ── Development stage with Caddy + hot reload ────────────────────────────────
FROM caddy:2-alpine AS development

# Install Node.js, npm, curl, and build tools for better-sqlite3
RUN apk add --no-cache nodejs npm curl python3 make g++

WORKDIR /app

# Create data directory for SQLite databases and file storage
RUN mkdir -p /data /data/files/blobs

# Copy package files and install dependencies
COPY package*.json ./
RUN npm ci

# Copy source code (will be overridden by volume mounts in dev)
COPY . .

# Shared startup script for both development and production
COPY start.sh /app/start.sh
RUN chmod +x /app/start.sh

EXPOSE 80
EXPOSE 443

CMD ["/app/start.sh"]

# ── Production stage with Caddy ──────────────────────────────────────────────
FROM caddy:2-alpine AS production

# Install Node.js and curl for backend and health checks
RUN apk add --no-cache nodejs npm curl

WORKDIR /app

# Create data directory for SQLite databases and file storage
RUN mkdir -p /data /data/files/blobs

# Copy Next.js standalone build  
COPY --from=builder /app/.next/standalone ./
COPY --from=builder /app/public ./public
COPY --from=builder /app/.next/static ./.next/static

# Copy better-sqlite3 native bindings (not bundled in standalone)
COPY --from=builder /app/node_modules/better-sqlite3 ./node_modules/better-sqlite3
COPY --from=builder /app/node_modules/bindings ./node_modules/bindings
COPY --from=builder /app/node_modules/file-uri-to-path ./node_modules/file-uri-to-path

# Copy and setup startup script (generates Caddyfile dynamically)
COPY start.sh /app/start.sh
RUN chmod +x /app/start.sh

EXPOSE 80
EXPOSE 443

CMD ["/app/start.sh"]