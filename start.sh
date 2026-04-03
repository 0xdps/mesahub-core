#!/bin/sh
set -e

# PORT      = external port Caddy listens on (set by Railway, default 80)
# BACKEND_PORT = internal port Next.js listens on (must differ from PORT)
PORT=${PORT:-80}
BACKEND_PORT=${BACKEND_PORT:-3000}

# Guard: if Railway injected PORT and BACKEND_PORT was not explicitly set to
# something different, the two would collide and Caddy would fail to bind.
# In that case, pick a safe internal port automatically.
if [ "$BACKEND_PORT" = "$PORT" ]; then
  BACKEND_PORT=3000
  if [ "$PORT" = "3000" ]; then
    BACKEND_PORT=3001
  fi
  echo "⚠️  BACKEND_PORT collision detected with PORT=$PORT — using BACKEND_PORT=$BACKEND_PORT"
fi

echo "📌 PORT=$PORT (Caddy external)  BACKEND_PORT=$BACKEND_PORT (Next.js internal)"

# Determine behavior based on NODE_ENV
if [ "$NODE_ENV" = "development" ]; then
    # === DEVELOPMENT MODE ===
    echo "🚀 Starting SQLite Hub in DEVELOPMENT mode (with hot reload)"
    
    # Create data directories
    mkdir -p /data /data/files/blobs
    
    # Run the plain Next.js dev server in-container; host Portless handles routing.
    echo "Starting Next.js dev server on port $BACKEND_PORT..."
    PORT=$BACKEND_PORT npx next dev -p $BACKEND_PORT -H 0.0.0.0 &
    # Restore PORT so the Caddyfile heredoc below uses the external port
    PORT=${PORT}
    BACKEND_PID=$!
    
    # Wait for dev server to be ready (longer timeout for dev)
    echo "Waiting for dev server to be ready on localhost:$BACKEND_PORT..."
    max_attempts=60
    attempt=0
    until curl -sf http://localhost:$BACKEND_PORT/api/health > /dev/null 2>&1; do
      attempt=$((attempt + 1))
      if [ $attempt -eq $max_attempts ]; then
        echo "❌ Dev server failed to start on port $BACKEND_PORT"
        kill $BACKEND_PID 2>/dev/null || true
        exit 1
      fi
      sleep 2
    done
    
    echo "✅ Dev server ready with hot reload enabled"
    
    # Generate Caddyfile for reverse proxy (sequential appends — safe in /bin/sh)
    CONTROL_ENABLED_VAL="$(echo "${ENABLE_CONTROL_DB:-false}" | tr '[:upper:]' '[:lower:]')"

    # ── Global options ────────────────────────────────────────────────────────
    cat > /tmp/Caddyfile <<EOF
{
	auto_https off
	admin off
}
EOF

    # ── Subdomain blocks — only added when ENABLE_CONTROL_DB=true ─────────────
    if [ "$CONTROL_ENABLED_VAL" = "true" ]; then
        cat >> /tmp/Caddyfile <<EOF

api.mesahub.app:$PORT {
	@double_slash path_regexp dslash ^//(.*)$
	rewrite @double_slash /{http.regexp.dslash.1}

	@no_api_prefix {
		not path /api/*
		not path /_next/*
	}
	rewrite @no_api_prefix /api{uri}

	handle /api/db/*/files/* {
		reverse_proxy localhost:$BACKEND_PORT {
			@sendfile header X-Sendfile *
			handle_response @sendfile {
				header {
					Content-Type {http.reverse_proxy.header.Content-Type}
					Content-Disposition {http.reverse_proxy.header.Content-Disposition}
					ETag {http.reverse_proxy.header.ETag}
					X-Content-Hash {http.reverse_proxy.header.X-Content-Hash}
					Vary {http.reverse_proxy.header.Vary}
					Access-Control-Allow-Origin {http.reverse_proxy.header.Access-Control-Allow-Origin}
					Access-Control-Allow-Methods {http.reverse_proxy.header.Access-Control-Allow-Methods}
					Access-Control-Allow-Headers {http.reverse_proxy.header.Access-Control-Allow-Headers}
					-X-Sendfile
				}
				root * /data/files/blobs
				rewrite * {http.reverse_proxy.header.X-Sendfile}
				file_server
			}
		}
	}

	handle {
		reverse_proxy localhost:$BACKEND_PORT
	}
}

admin.mesahub.app:$PORT {
	handle /api/* {
		respond "Not found" 404
	}

	handle {
		reverse_proxy localhost:$BACKEND_PORT
	}
}
EOF
    fi

    # ── Default catch-all ─────────────────────────────────────────────────────
    cat >> /tmp/Caddyfile <<EOF

:$PORT {
	# Caddy-native health check — responds before Next.js is involved
	handle /health {
		respond 200
	}

	@double_slash path_regexp dslash ^//(.*)$
	rewrite @double_slash /{http.regexp.dslash.1}

	handle /api/db/*/files/* {
		reverse_proxy localhost:$BACKEND_PORT {
			@sendfile header X-Sendfile *
			handle_response @sendfile {
				header {
					Content-Type {http.reverse_proxy.header.Content-Type}
					Content-Disposition {http.reverse_proxy.header.Content-Disposition}
					ETag {http.reverse_proxy.header.ETag}
					X-Content-Hash {http.reverse_proxy.header.X-Content-Hash}
					Vary {http.reverse_proxy.header.Vary}
					Access-Control-Allow-Origin {http.reverse_proxy.header.Access-Control-Allow-Origin}
					Access-Control-Allow-Methods {http.reverse_proxy.header.Access-Control-Allow-Methods}
					Access-Control-Allow-Headers {http.reverse_proxy.header.Access-Control-Allow-Headers}
					-X-Sendfile
				}
				root * /data/files/blobs
				rewrite * {http.reverse_proxy.header.X-Sendfile}
				file_server
			}
		}
	}

	handle /*/file/* {
		reverse_proxy localhost:$BACKEND_PORT {
			@sendfile header X-Sendfile *
			handle_response @sendfile {
				header {
					Content-Type {http.reverse_proxy.header.Content-Type}
					Content-Disposition {http.reverse_proxy.header.Content-Disposition}
					ETag {http.reverse_proxy.header.ETag}
					X-Content-Hash {http.reverse_proxy.header.X-Content-Hash}
					Vary {http.reverse_proxy.header.Vary}
					Access-Control-Allow-Origin {http.reverse_proxy.header.Access-Control-Allow-Origin}
					Access-Control-Allow-Methods {http.reverse_proxy.header.Access-Control-Allow-Methods}
					Access-Control-Allow-Headers {http.reverse_proxy.header.Access-Control-Allow-Headers}
					-X-Sendfile
				}
				root * /data/files/blobs
				rewrite * {http.reverse_proxy.header.X-Sendfile}
				file_server
			}
		}
	}

	handle {
		reverse_proxy localhost:$BACKEND_PORT
	}
}
EOF

    echo "Starting Caddy reverse proxy on port $PORT..."
    caddy fmt --overwrite /tmp/Caddyfile
    caddy run --config /tmp/Caddyfile

else
    # === PRODUCTION MODE ===
    echo "🚀 Starting SQLite Hub in PRODUCTION mode"
    
    # Create data directories
    mkdir -p /data /data/files/blobs
    
    # Start Next.js backend (production)
    echo "Starting Next.js server on port $BACKEND_PORT..."
    
    # Check if server.js exists
    if [ ! -f /app/nextjs/server.js ]; then
        echo "❌ Error: server.js not found at /app/nextjs/server.js"
        echo "Checking alternative locations..."
        find /app -name "server.js" -type f 2>/dev/null || echo "No server.js found anywhere in /app"
        exit 1
    fi
    
    # Run Next.js on BACKEND_PORT only; do NOT export PORT=BACKEND_PORT to the
    # shell or Caddy would try to bind the same port as Next.js.
    # HOSTNAME=0.0.0.0 ensures Next.js binds to all interfaces (not just the
    # container hostname), so curl http://localhost:$BACKEND_PORT succeeds.
    PORT=$BACKEND_PORT HOSTNAME=0.0.0.0 node /app/nextjs/server.js &
    BACKEND_PID=$!
    
    # Wait for backend to be ready (quicker for prod)
    echo "Waiting for backend to be ready on localhost:$BACKEND_PORT..."
    max_attempts=30
    attempt=0
    until curl -sf http://localhost:$BACKEND_PORT/api/health > /dev/null 2>&1; do
      attempt=$((attempt + 1))
      if [ $attempt -eq $max_attempts ]; then
        echo "❌ Backend failed to start on port $BACKEND_PORT"
        kill $BACKEND_PID 2>/dev/null || true
        exit 1
      fi
      sleep 1
    done
    
    echo "✅ Backend ready on port $BACKEND_PORT"
    
    # Generate Caddyfile for reverse proxy (sequential appends — safe in /bin/sh)
    CONTROL_ENABLED_VAL="$(echo "${ENABLE_CONTROL_DB:-false}" | tr '[:upper:]' '[:lower:]')"

    # ── Global options ────────────────────────────────────────────────────────
    cat > /tmp/Caddyfile <<EOF
{
	auto_https off
	admin off
}
EOF

    # ── Subdomain blocks — only added when ENABLE_CONTROL_DB=true ─────────────
    if [ "$CONTROL_ENABLED_VAL" = "true" ]; then
        cat >> /tmp/Caddyfile <<EOF

api.mesahub.app:$PORT {
	@double_slash path_regexp dslash ^//(.*)$
	rewrite @double_slash /{http.regexp.dslash.1}

	@no_api_prefix {
		not path /api/*
		not path /_next/*
	}
	rewrite @no_api_prefix /api{uri}

	handle /api/db/*/files/* {
		reverse_proxy localhost:$BACKEND_PORT {
			@sendfile header X-Sendfile *
			handle_response @sendfile {
				header {
					Content-Type {http.reverse_proxy.header.Content-Type}
					Content-Disposition {http.reverse_proxy.header.Content-Disposition}
					ETag {http.reverse_proxy.header.ETag}
					X-Content-Hash {http.reverse_proxy.header.X-Content-Hash}
					Vary {http.reverse_proxy.header.Vary}
					Access-Control-Allow-Origin {http.reverse_proxy.header.Access-Control-Allow-Origin}
					Access-Control-Allow-Methods {http.reverse_proxy.header.Access-Control-Allow-Methods}
					Access-Control-Allow-Headers {http.reverse_proxy.header.Access-Control-Allow-Headers}
					-X-Sendfile
				}
				root * /data/files/blobs
				rewrite * {http.reverse_proxy.header.X-Sendfile}
				file_server
			}
		}
	}

	handle {
		reverse_proxy localhost:$BACKEND_PORT
	}
}

admin.mesahub.app:$PORT {
	handle /api/* {
		respond "Not found" 404
	}

	handle {
		reverse_proxy localhost:$BACKEND_PORT
	}
}
EOF
    fi

    # ── Default catch-all ─────────────────────────────────────────────────────
    cat >> /tmp/Caddyfile <<EOF

:$PORT {
	# Caddy-native health check — responds before Next.js is involved
	handle /health {
		respond 200
	}

	# Silently normalize double leading slashes (e.g. //foo → /foo).
	# Without this, Caddy issues a redirect which strips CORS headers and
	# breaks preflight requests from cross-origin clients.
	@double_slash path_regexp dslash ^//(.*)$
	rewrite @double_slash /{http.regexp.dslash.1}

	# API routes and file operations with X-Sendfile acceleration
	handle /api/db/*/files/* {
		reverse_proxy localhost:$BACKEND_PORT {
			@sendfile header X-Sendfile *
			handle_response @sendfile {
				# Copy metadata headers from upstream; do NOT copy Content-Length —
				# the upstream body is empty (X-Sendfile pattern) so Content-Length
				# would lie and cause Caddy to stall waiting for bytes. file_server
				# sets the correct Content-Length from the actual file on disk.
				header {
					Content-Type {http.reverse_proxy.header.Content-Type}
					Content-Disposition {http.reverse_proxy.header.Content-Disposition}
					ETag {http.reverse_proxy.header.ETag}
					X-Content-Hash {http.reverse_proxy.header.X-Content-Hash}
					Vary {http.reverse_proxy.header.Vary}
					Access-Control-Allow-Origin {http.reverse_proxy.header.Access-Control-Allow-Origin}
					Access-Control-Allow-Methods {http.reverse_proxy.header.Access-Control-Allow-Methods}
					Access-Control-Allow-Headers {http.reverse_proxy.header.Access-Control-Allow-Headers}
					-X-Sendfile
				}
				root * /data/files/blobs
				rewrite * {http.reverse_proxy.header.X-Sendfile}
				file_server
			}
		}
	}

	handle /*/file/* {
		reverse_proxy localhost:$BACKEND_PORT {
			@sendfile header X-Sendfile *
			handle_response @sendfile {
				header {
					Content-Type {http.reverse_proxy.header.Content-Type}
					Content-Disposition {http.reverse_proxy.header.Content-Disposition}
					ETag {http.reverse_proxy.header.ETag}
					X-Content-Hash {http.reverse_proxy.header.X-Content-Hash}
					Vary {http.reverse_proxy.header.Vary}
					Access-Control-Allow-Origin {http.reverse_proxy.header.Access-Control-Allow-Origin}
					Access-Control-Allow-Methods {http.reverse_proxy.header.Access-Control-Allow-Methods}
					Access-Control-Allow-Headers {http.reverse_proxy.header.Access-Control-Allow-Headers}
					-X-Sendfile
				}
				root * /data/files/blobs
				rewrite * {http.reverse_proxy.header.X-Sendfile}
				file_server
			}
		}
	}

	handle {
		reverse_proxy localhost:$BACKEND_PORT
	}
}
EOF

    echo "Formatting Caddyfile..."
    caddy fmt --overwrite /tmp/Caddyfile

    echo "Validating Caddyfile..."
    caddy validate --config /tmp/Caddyfile || { echo "❌ Caddyfile validation failed"; exit 1; }

    echo "Starting Caddy on port $PORT..."
    # Start Caddy in foreground with the generated config
    exec caddy run --config /tmp/Caddyfile
fi

