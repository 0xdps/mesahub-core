// Package control — apikeys.go handles API key management.
//
//	GET    /api/control/apikeys       — list user's active API keys
//	POST   /api/control/apikeys       — create API key (returns raw key once)
//	DELETE /api/control/apikeys/{id}  — revoke API key
package control

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"
)

// ListAPIKeys handles GET /api/control/apikeys.
func (h *Handler) ListAPIKeys(w http.ResponseWriter, r *http.Request) {
	user := UserFromCtx(r.Context())
	keys, err := h.cdb.ListAPIKeys(user.ID)
	if err != nil {
		log.Error().Err(err).Msg("[control/apikeys] ListAPIKeys")
		writeError(w, http.StatusInternalServerError, "failed to list API keys")
		return
	}
	if keys == nil {
		keys = []APIKey{}
	}
	writeJSON(w, http.StatusOK, keys)
}

// CreateAPIKey handles POST /api/control/apikeys.
// Returns the raw key value in the response body — shown once only.
func (h *Handler) CreateAPIKey(w http.ResponseWriter, r *http.Request) {
	user := UserFromCtx(r.Context())

	var body struct {
		Name        string   `json:"name"`
		Scope       string   `json:"scope"`        // "all" | "databases"
		DatabaseIDs []string `json:"database_ids"` // required when scope == "databases"
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if body.Scope == "" {
		body.Scope = "all"
	}
	if body.Scope != "all" && body.Scope != "databases" {
		writeError(w, http.StatusBadRequest, "scope must be 'all' or 'databases'")
		return
	}
	if body.Scope == "databases" && len(body.DatabaseIDs) == 0 {
		writeError(w, http.StatusBadRequest, "database_ids required when scope is 'databases'")
		return
	}

	key, rawKey, err := h.cdb.CreateAPIKey(user.ID, body.Name, body.Scope, body.DatabaseIDs)
	if err != nil {
		log.Error().Err(err).Msg("[control/apikeys] CreateAPIKey")
		writeError(w, http.StatusInternalServerError, "failed to create API key")
		return
	}

	log.Info().Str("user", user.Email).Str("key_id", key.ID).Msg("[control/apikeys] API key created")
	writeJSON(w, http.StatusCreated, map[string]any{
		"key":     key,
		"raw_key": rawKey, // shown once
	})
}

// RevokeAPIKey handles DELETE /api/control/apikeys/{id}.
func (h *Handler) RevokeAPIKey(w http.ResponseWriter, r *http.Request) {
	user := UserFromCtx(r.Context())
	id := chi.URLParam(r, "id")

	// Fetch hash before revoking (for cache invalidation).
	keyHash, _ := h.cdb.GetAPIKeyHash(id)

	if err := h.cdb.RevokeAPIKey(id, user.ID); err != nil {
		log.Warn().Err(err).Str("id", id).Msg("[control/apikeys] RevokeAPIKey")
		writeError(w, http.StatusNotFound, "API key not found or not owned by you")
		return
	}

	// Evict from cache so the template's middleware sees the revocation immediately.
	if keyHash != "" {
		_ = h.cache.DeleteAPIKey(r.Context(), keyHash)
	}

	log.Info().Str("user", user.Email).Str("key_id", id).Msg("[control/apikeys] API key revoked")
	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}
