// Package auth — handler.go provides the HTTP handlers for the auth endpoints.
package auth

import (
	"encoding/json"
	"net/http"

	"github.com/rs/zerolog/log"

	"github.com/0xdps/sqlite-hub-template/cache"
	"github.com/0xdps/sqlite-hub-template/config"
)

// Handler bundles the auth HTTP handlers with their dependencies.
type Handler struct {
	cfg   *config.Config
	cache cache.Client
}

// NewHandler returns a Handler ready to serve auth routes.
func NewHandler(cfg *config.Config, c cache.Client) *Handler {
	return &Handler{cfg: cfg, cache: c}
}

// Login handles POST /api/auth/login.
// Accepts {"token": "<value>"} and issues a session cookie when the token
// matches ADMIN_TOKEN. Mirrors the Next.js login/route.ts behaviour.
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Token == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "token is required"})
		return
	}

	if !TimingSafeMatch(body.Token, h.cfg.AdminToken) {
		log.Warn().Msg("[auth] login failed — invalid token")
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Invalid token"})
		return
	}

	if err := IssueSession(w, r, h.cfg, h.cache); err != nil {
		log.Error().Err(err).Msg("[auth] IssueSession failed")
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "session error"})
		return
	}

	log.Info().Msg("[auth] admin login successful")
	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// Logout handles POST /api/auth/logout.
// Destroys the active session and clears the cookie.
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	DestroySession(w, r, h.cfg, h.cache)
	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// Me handles GET /api/auth/me.
// Protected by RequireAdmin — returns the logged-in status.
func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"loggedIn": true})
}
