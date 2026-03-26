#!/bin/sh
set -e

# Determine behavior based on NODE_ENV
if [ "$NODE_ENV" = "development" ]; then
    # === DEVELOPMENT MODE ===
    echo "🚀 Starting SQLite Hub in DEVELOPMENT mode (with hot reload)"
    
    # Create data directories
    mkdir -p /data /data/files/blobs
    
    # Run the plain Next.js dev server in-container; host Portless handles routing.
    echo "Starting Next.js dev server on port $BACKEND_PORT..."
    PORT=$BACKEND_PORT npx next dev -p $BACKEND_PORT -H 0.0.0.0 &
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
    
    # Generate Caddyfile for reverse proxy
    cat > /tmp/Caddyfile <<EOF
{
	auto_https off
	admin off
	log default {
		output stdout
		format json
		level DEBUG
	}
}

:$PORT {
	log {
		output stdout
		format json
		level DEBUG
	}

	# Health check endpoint
	handle /api/health {
		reverse_proxy localhost:$BACKEND_PORT
	}
	
	# File operations with X-Sendfile acceleration
	handle /api/db/*/files/* {
		reverse_proxy localhost:$BACKEND_PORT {
			header_up X-Forwarded-For {remote_host}
			@sendfile header X-Sendfile *
			handle_response @sendfile {
				header {
					Content-Type {http.reverse_proxy.header.Content-Type}
					Content-Disposition {http.reverse_proxy.header.Content-Disposition}
					Content-Length {http.reverse_proxy.header.Content-Length}
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
	
	# Shortlink file routes with X-Sendfile acceleration
	handle /*/file/* {
		reverse_proxy localhost:$BACKEND_PORT {
			@sendfile header X-Sendfile *
			handle_response @sendfile {
				# Preserve response headers from Node.js (CORS, content-type, etc)
				header {
					# Copy CORS headers from reverse proxy response
					Vary {http.reverse_proxy.header.Vary}
					Access-Control-Allow-Origin {http.reverse_proxy.header.Access-Control-Allow-Origin}
					Access-Control-Allow-Methods {http.reverse_proxy.header.Access-Control-Allow-Methods}
					Access-Control-Allow-Headers {http.reverse_proxy.header.Access-Control-Allow-Headers}
					X-Content-Hash {http.reverse_proxy.header.X-Content-Hash}
					# Remove X-Sendfile so it doesn't leak to client
					-X-Sendfile
				}
				root * /data/files/blobs
				rewrite * {http.reverse_proxy.header.X-Sendfile}
				file_server
			}
		}
	}
	
	# Everything else to Next.js
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
    
    # Debug: List /app directory contents
    echo "📂 Contents of /app directory:"
    ls -la /app/ | head -20
    
    # Create data directories
    mkdir -p /data /data/files/blobs
    
    # Start Next.js backend (production)
    echo "Starting Next.js server on port $BACKEND_PORT..."
    
    # Check if server.js exists
    if [ ! -f /app/server.js ]; then
        echo "❌ Error: server.js not found at /app/server.js"
        echo "Checking alternative locations..."
        find /app -name "server.js" -type f 2>/dev/null || echo "No server.js found anywhere in /app"
        exit 1
    fi
    
    PORT=$BACKEND_PORT node /app/server.js &
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
    
    # Generate Caddyfile for reverse proxy
    cat > /tmp/Caddyfile <<EOF
{
	auto_https off
	admin off
	log default {
		output stdout
		format json
		level DEBUG
	}
}

:$PORT {
	log {
		output stdout
		format json
		level DEBUG
	}

	# Health check endpoint
	handle /api/health {
		reverse_proxy localhost:$BACKEND_PORT
	}

	# API routes and file operations with X-Sendfile acceleration
	handle /api/db/*/files/* {
		reverse_proxy localhost:$BACKEND_PORT {
			header_up X-Forwarded-For {remote_host}
			@sendfile header X-Sendfile *
			handle_response @sendfile {
				header {
					Content-Type {http.reverse_proxy.header.Content-Type}
					Content-Disposition {http.reverse_proxy.header.Content-Disposition}
					Content-Length {http.reverse_proxy.header.Content-Length}
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
	
	# Shortlink file routes with X-Sendfile acceleration
	handle /*/file/* {
		reverse_proxy localhost:$BACKEND_PORT {
			@sendfile header X-Sendfile *
			handle_response @sendfile {
				# Preserve response headers from Node.js (CORS, content-type, etc)
				header {
					# Copy CORS headers from reverse proxy response
					Vary {http.reverse_proxy.header.Vary}
					Access-Control-Allow-Origin {http.reverse_proxy.header.Access-Control-Allow-Origin}
					Access-Control-Allow-Methods {http.reverse_proxy.header.Access-Control-Allow-Methods}
					Access-Control-Allow-Headers {http.reverse_proxy.header.Access-Control-Allow-Headers}
					X-Content-Hash {http.reverse_proxy.header.X-Content-Hash}
					# Remove X-Sendfile so it doesn't leak to client
					-X-Sendfile
				}
				root * /data/files/blobs
				rewrite * {http.reverse_proxy.header.X-Sendfile}
				file_server
			}
		}
	}

	# Everything else proxies to Next.js (API, pages, static)
	handle {
		reverse_proxy localhost:$BACKEND_PORT
	}
}
EOF
    
    echo "Formatting Caddyfile..."
    caddy fmt --overwrite /tmp/Caddyfile
    
    echo "Starting Caddy on port $PORT..."
    # Start Caddy in foreground with the generated config
    caddy run --config /tmp/Caddyfile
fi

