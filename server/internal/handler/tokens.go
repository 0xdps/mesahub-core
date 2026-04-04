// Package handler — tokens.go handles /api/db/:name/tokens/files routes.
package handler

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"github.com/0xdps/sqlite-hub/server/internal/auth"
	"github.com/0xdps/sqlite-hub/server/internal/cache"
	"github.com/0xdps/sqlite-hub/server/internal/config"
	"github.com/0xdps/sqlite-hub/server/internal/db"
	"github.com/0xdps/sqlite-hub/server/internal/filetoken"
)

// TokensHandler holds dependencies for the token route group.
type TokensHandler struct {
	cfg      *config.Config
	registry *db.Registry
	cache    cache.Client
}

// NewTokensHandler creates a TokensHandler.
func NewTokensHandler(cfg *config.Config, registry *db.Registry, c cache.Client) *TokensHandler {
	return &TokensHandler{cfg: cfg, registry: registry, cache: c}
}

// CreateToken handles POST /api/db/:name/tokens/files.
func (h *TokensHandler) CreateToken(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	rec, err := h.registry.GetDatabase(name)
	if err != nil || rec == nil {
		ErrorJSON(w, http.StatusNotFound, "Database not found")
		return
	}
	if code, msg := auth.AuthorizeDB(r, h.cfg, h.cache, rec); code != 0 {
		ErrorJSON(w, code, msg)
		return
	}

	var body struct {
		Scope       string `json:"scope"`
		ExpiresIn   int    `json:"expires_in"`
		Description string `json:"description"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.Scope != "" && body.Scope != "files:read" {
		ErrorJSON(w, http.StatusBadRequest, "scope must be 'files:read'")
		return
	}

	result, err := filetoken.Create(name, body.ExpiresIn)
	if err != nil {
		ErrorJSON(w, http.StatusInternalServerError, err.Error())
		return
	}

	log.Info().Str("db", name).Str("token_id", result.TokenID).Msg("[tokens] created file token")
	writeJSON(w, http.StatusCreated, map[string]any{
		"token_id":   result.TokenID,
		"token":      result.Token,
		"expires_at": result.ExpiresAt.UTC().Format(time.RFC3339),
		"expires_in": result.ExpiresIn,
		"scope":      result.Scope,
	})
}

// RevokeToken handles POST /api/db/:name/tokens/files/revoke.
func (h *TokensHandler) RevokeToken(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	rec, err := h.registry.GetDatabase(name)
	if err != nil || rec == nil {
		ErrorJSON(w, http.StatusNotFound, "Database not found")
		return
	}
	if code, msg := auth.AuthorizeDB(r, h.cfg, h.cache, rec); code != 0 {
		ErrorJSON(w, code, msg)
		return
	}

	var body struct {
		Token     string `json:"token"`
		TokenID   string `json:"token_id"`
		ExpiresAt string `json:"expires_at"` // ISO-8601, required when token_id provided directly
		Reason    string `json:"reason"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}

	var tokenID, expiresAt string

	if body.Token != "" {
		payload, verifyErr := filetoken.Verify(body.Token)
		if verifyErr != nil || payload == nil {
			ErrorJSON(w, http.StatusBadRequest, "Invalid or expired token")
			return
		}
		tokenID = payload.TokenID
		expiresAt = time.Unix(payload.ExpiresAt, 0).UTC().Format(time.RFC3339)
	} else if body.TokenID != "" {
		if body.ExpiresAt == "" {
			ErrorJSON(w, http.StatusBadRequest, "expires_at is required when token_id is provided")
			return
		}
		tokenID = body.TokenID
		expiresAt = body.ExpiresAt
	} else {
		ErrorJSON(w, http.StatusBadRequest, "token or token_id is required")
		return
	}

	var reasonPtr *string
	if body.Reason != "" {
		reasonPtr = &body.Reason
	}

	if err := h.registry.RevokeFileToken(tokenID, name, expiresAt, reasonPtr); err != nil {
		ErrorJSON(w, http.StatusInternalServerError, err.Error())
		return
	}
	log.Info().Str("db", name).Str("token_id", tokenID).Msg("[tokens] revoked file token")
	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}
