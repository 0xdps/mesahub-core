FROM node:20-alpine AS builder

WORKDIR /app

# Install native build tools required by better-sqlite3
RUN apk add --no-cache python3 make g++

COPY package*.json ./
RUN npm ci

COPY . .
RUN npm run build

# ── Runtime image ────────────────────────────────────────────────────────────
FROM node:20-alpine AS runner

WORKDIR /app

ENV NODE_ENV=production
# "::" binds to both IPv4 and IPv6 — required for Railway private networking
# which resolves internal hostnames to IPv6 (fd12::/7) first.
ENV HOSTNAME="::"
# PORT is injected by Railway at runtime

# Pre-create the data directory (overridden by Railway volume at runtime)
RUN mkdir -p /data

COPY --from=builder /app/.next/standalone ./
COPY --from=builder /app/public ./public
COPY --from=builder /app/.next/static ./.next/static

# better-sqlite3 native bindings are not bundled by Next.js standalone —
# copy the full package so the .node file resolves at runtime.
COPY --from=builder /app/node_modules/better-sqlite3 ./node_modules/better-sqlite3
COPY --from=builder /app/node_modules/bindings ./node_modules/bindings
COPY --from=builder /app/node_modules/file-uri-to-path ./node_modules/file-uri-to-path

EXPOSE 8080

CMD ["node", "server.js"]