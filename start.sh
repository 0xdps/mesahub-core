#!/bin/sh
set -e

# PORT       — external port Caddy listens on (set by Railway, default 80)
# Go and Next.js internal ports are fixed.
PORT=${PORT:-80}
GO_PORT=3000
NEXTJS_PORT=3001

# Optional: set both to enable split-domain routing in control mode.
# Example: API_HOSTNAME=api.yourdomain.com  ADMIN_HOSTNAME=admin.yourdomain.com
API_HOSTNAME="${API_HOSTNAME:-}"
ADMIN_HOSTNAME="${ADMIN_HOSTNAME:-}"

echo "PORT=$PORT (Caddy)  Go=:$GO_PORT  Next.js=:$NEXTJS_PORT"

# ---------------------------------------------------------------------------
if [ "$NODE_ENV" = "development" ]; then
# ---------------------------------------------------------------------------
    echo "Starting SQLite Hub in DEVELOPMENT mode"
    mkdir -p /data /data/files/blobs

    echo "Starting Go server on :$GO_PORT ..."
    PORT=$GO_PORT /app/server/sqlite-hub-server &
    GO_PID=$!

    echo "Starting Next.js dev server on :$NEXTJS_PORT ..."
    cd /app/dashboard && PORT=$NEXTJS_PORT pnpm next dev --port $NEXTJS_PORT --hostname 0.0.0.0 &
    NEXTJS_PID=$!

    echo "Waiting for Go server ..."
    max_attempts=30
    attempt=0
    until curl -sf http://localhost:$GO_PORT/api/health > /dev/null 2>&1; do
        attempt=$((attempt + 1))
        if [ $attempt -eq $max_attempts ]; then
            echo "Go server failed to start on port $GO_PORT"
            kill $GO_PID $NEXTJS_PID 2>/dev/null || true
            exit 1
        fi
        sleep 2
    done
    echo "Go server ready"

    echo "Waiting for Next.js dev server ..."
    max_attempts=60
    attempt=0
    until curl -sf http://localhost:$NEXTJS_PORT/api/health > /dev/null 2>&1; do
        attempt=$((attempt + 1))
        if [ $attempt -eq $max_attempts ]; then
            echo "Next.js dev server failed to start on port $NEXTJS_PORT"
            kill $GO_PID $NEXTJS_PID 2>/dev/null || true
            exit 1
        fi
        sleep 2
    done
    echo "Next.js dev server ready"

    cat > /tmp/Caddyfile <<EOF
{
    auto_https off
    admin off
}

:${PORT} {
    @dslash path_regexp dslash ^//(.*)$
    rewrite @dslash /{http.regexp.dslash.1}

    # File downloads (Go sets X-Sendfile, Caddy serves blob)
    # Everything -> Next.js dev server
    handle {
        reverse_proxy localhost:${NEXTJS_PORT}
    }
}
EOF

    caddy fmt --overwrite /tmp/Caddyfile
    caddy run --config /tmp/Caddyfile

# ---------------------------------------------------------------------------
else
# ---------------------------------------------------------------------------
    echo "Starting SQLite Hub in PRODUCTION mode"
    mkdir -p /data /data/files/blobs

    if [ ! -f /app/dashboard/.next/standalone/server.js ]; then
        echo "Next.js standalone build not found at /app/dashboard/.next/standalone/server.js"
        exit 1
    fi

    if [ ! -f /app/server/sqlite-hub-server ]; then
        echo "Go server binary not found at /app/server/sqlite-hub-server"
        exit 1
    fi

    echo "Starting Go server and Next.js via supervisord ..."
    supervisord -c /etc/supervisor/conf.d/sqlite-hub.conf &
    SUPERVISOR_PID=$!

    echo "Waiting for Go server on :$GO_PORT ..."
    max_attempts=30
    attempt=0
    until curl -sf http://localhost:$GO_PORT/api/health > /dev/null 2>&1; do
        attempt=$((attempt + 1))
        if [ $attempt -eq $max_attempts ]; then
            echo "Go server failed to start on port $GO_PORT"
            kill $SUPERVISOR_PID 2>/dev/null || true
            exit 1
        fi
        sleep 2
    done
    echo "Go server ready"

    echo "Waiting for Next.js server on :$NEXTJS_PORT ..."
    max_attempts=60
    attempt=0
    until curl -sf http://localhost:$NEXTJS_PORT/api/health > /dev/null 2>&1; do
        attempt=$((attempt + 1))
        if [ $attempt -eq $max_attempts ]; then
            echo "Next.js server failed to start on port $NEXTJS_PORT"
            kill $SUPERVISOR_PID 2>/dev/null || true
            exit 1
        fi
        sleep 2
    done
    echo "Next.js server ready"

    CONTROL_ENABLED_VAL="$(echo "${ENABLE_CONTROL_DB:-false}" | tr '[:upper:]' '[:lower:]')"

    # ── Caddyfile header ──────────────────────────────────────────────────────
    cat > /tmp/Caddyfile <<EOF
{
    auto_https off
    admin off
}

:${PORT} {
    @dslash path_regexp dslash ^//(.*)$
    rewrite @dslash /{http.regexp.dslash.1}
EOF

    # ── Named-host routing (only when split domains are configured) ───────────
    if [ "$CONTROL_ENABLED_VAL" = "true" ] && [ -n "$API_HOSTNAME" ] && [ -n "$ADMIN_HOSTNAME" ]; then
        cat >> /tmp/Caddyfile <<EOF

    # --- ${API_HOSTNAME} -> Go API server ---
    # All callers (SDK, control plane) omit the /api prefix — prepend it here.
    @api_host host ${API_HOSTNAME}
    handle @api_host {
        rewrite * /api{uri}
        reverse_proxy localhost:${GO_PORT}
    }

    # --- ${ADMIN_HOSTNAME} -> Next.js admin UI ---
    @admin_host host ${ADMIN_HOSTNAME}
    handle @admin_host {
        reverse_proxy localhost:${NEXTJS_PORT}
    }
EOF
    fi

    # ── Path-based fallback routing (standalone + Railway preview URLs) ───────
    cat >> /tmp/Caddyfile <<EOF

    # Everything -> Next.js
    handle {
        reverse_proxy localhost:${NEXTJS_PORT}
    }
}
EOF

    echo "Formatting Caddyfile ..."
    caddy fmt --overwrite /tmp/Caddyfile

    echo "Validating Caddyfile ..."
    caddy validate --config /tmp/Caddyfile || { echo "Caddyfile validation failed"; exit 1; }

    echo "Starting Caddy on port $PORT ..."
    exec caddy run --config /tmp/Caddyfile
fi
