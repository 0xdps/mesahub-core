#!/bin/sh
set -e

# PORT       — external port Caddy listens on (set by Railway, default 80)
# Go internal port is fixed and must not be changed.
PORT=${PORT:-80}
GO_PORT=3000
STATIC_PORT=3001  # Vite dev server (development only)

# Optional: set both to enable split-domain routing in control mode.
# Example: API_HOSTNAME=api.yourdomain.com  ADMIN_HOSTNAME=admin.yourdomain.com
API_HOSTNAME="${API_HOSTNAME:-}"
ADMIN_HOSTNAME="${ADMIN_HOSTNAME:-}"

echo "PORT=$PORT (Caddy)  Go=:$GO_PORT"

# ---------------------------------------------------------------------------
# Shared helper — writes an X-Sendfile handle_response block to stdout.
# Go sets the X-Sendfile header; Caddy intercepts it and serves the blob
# directly from /data/files/blobs, so Go never streams the binary itself.
# ---------------------------------------------------------------------------
sendfile_response() {
    cat <<'SFBLOCK'
            @sendfile header X-Sendfile *
            handle_response @sendfile {
                header {
                    Content-Type        {http.reverse_proxy.header.Content-Type}
                    Content-Disposition {http.reverse_proxy.header.Content-Disposition}
                    ETag                {http.reverse_proxy.header.ETag}
                    X-Content-Hash      {http.reverse_proxy.header.X-Content-Hash}
                    -X-Sendfile
                }
                root * /data/files/blobs
                rewrite * {http.reverse_proxy.header.X-Sendfile}
                file_server
            }
SFBLOCK
}

# ---------------------------------------------------------------------------
if [ "$NODE_ENV" = "development" ]; then
# ---------------------------------------------------------------------------
    echo "Starting SQLite Hub in DEVELOPMENT mode"
    mkdir -p /data /data/files/blobs

    # Start Vite dev server (Go must be started separately — see docker-compose.dev.yml)
    echo "Starting Vite dev server on :$STATIC_PORT ..."
    PORT=$STATIC_PORT pnpm vite --port $STATIC_PORT --host 0.0.0.0 &
    VITE_PID=$!

    echo "Waiting for Vite dev server ..."
    max_attempts=60
    attempt=0
    until curl -sf http://localhost:$STATIC_PORT > /dev/null 2>&1; do
        attempt=$((attempt + 1))
        if [ $attempt -eq $max_attempts ]; then
            echo "Vite dev server failed to start on port $STATIC_PORT"
            kill $VITE_PID 2>/dev/null || true
            exit 1
        fi
        sleep 2
    done
    echo "Vite dev server ready"

    cat > /tmp/Caddyfile <<EOF
{
    auto_https off
    admin off
}

:${PORT} {
    @dslash path_regexp dslash ^//(.*)$
    rewrite @dslash /{http.regexp.dslash.1}

    # File downloads (Go sets X-Sendfile, Caddy serves blob)
    handle /api/db/*/files/* {
        reverse_proxy localhost:${GO_PORT} {
$(sendfile_response)
        }
    }

    # Public file shortlinks (Go handles these once Phase 2 route is added)
    handle /*/file/* {
        reverse_proxy localhost:${GO_PORT} {
$(sendfile_response)
        }
    }

    # All API traffic -> Go
    handle /api/* {
        reverse_proxy localhost:${GO_PORT}
    }

    # Everything else -> Vite dev server
    handle {
        reverse_proxy localhost:${STATIC_PORT}
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

    if [ ! -f /app/dist/index.html ]; then
        echo "Static build not found at /app/dist/index.html"
        exit 1
    fi
    if [ ! -f /app/server/sqlite-hub-server ]; then
        echo "Go binary not found at /app/server/sqlite-hub-server"
        exit 1
    fi

    echo "Starting Go server via supervisord ..."
    supervisord -c /etc/supervisor/conf.d/sqlite-hub.conf &
    SUPERVISOR_PID=$!

    # Wait for Go first — it boots faster.
    echo "Waiting for Go server on :$GO_PORT ..."
    max_attempts=60
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
    @api_host host ${API_HOSTNAME}
    handle @api_host {
        handle /api/db/*/files/* {
            reverse_proxy localhost:${GO_PORT} {
$(sendfile_response)
            }
        }
        handle /*/file/* {
            reverse_proxy localhost:${GO_PORT} {
$(sendfile_response)
            }
        }
        # All traffic on the API hostname goes to Go
        handle {
            reverse_proxy localhost:${GO_PORT}
        }
    }

    # --- ${ADMIN_HOSTNAME} -> static admin UI ---
    @admin_host host ${ADMIN_HOSTNAME}
    handle @admin_host {
        root * /app/dist
        try_files {path} /index.html
        file_server
    }
EOF
    fi

    # ── Path-based fallback routing (standalone + Railway preview URLs) ───────
    cat >> /tmp/Caddyfile <<EOF

    # File downloads (X-Sendfile)
    handle /api/db/*/files/* {
        reverse_proxy localhost:${GO_PORT} {
$(sendfile_response)
        }
    }

    # Public file shortlinks
    handle /*/file/* {
        reverse_proxy localhost:${GO_PORT} {
$(sendfile_response)
        }
    }

    # All API traffic -> Go
    handle /api/* {
        reverse_proxy localhost:${GO_PORT}
    }

    # Everything else -> static admin UI (SPA)
    handle {
        root * /app/dist
        try_files {path} /index.html
        file_server
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
