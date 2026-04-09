// Package handler — internal.go handles /api/internal/* routes.
//
// These endpoints are exclusively for control-plane → template callbacks.
// They are protected by RequireControlPlane (CONTROL_PLANE_SECRET) and must
// never be exposed to end-users or API key holders.
package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"github.com/0xdps/sqlite-hub/server/internal/cache"
)

// InternalHandler holds dependencies for internal control-plane callbacks.
type InternalHandler struct {
	cache cache.Client
}

// NewInternalHandler creates an InternalHandler.
func NewInternalHandler(c cache.Client) *InternalHandler {
	return &InternalHandler{cache: c}
}

// InvalidateAPIKey handles DELETE /api/internal/cache/apikey/{hash}.
//
// Control calls this immediately after revoking or deleting an API key so that
// template evicts the cached entry from both L1 and Redis without waiting for
// the TTL to expire. The path parameter is the hex-encoded SHA-256 hash of the
// raw key — the same value stored in api_keys.key_hash in control.db.
func (h *InternalHandler) InvalidateAPIKey(w http.ResponseWriter, r *http.Request) {
	keyHash := chi.URLParam(r, "hash")
	if keyHash == "" {
		ErrorJSON(w, http.StatusBadRequest, "hash is required")
		return
	}
	if len(keyHash) < 8 {
		ErrorJSON(w, http.StatusBadRequest, "invalid hash")
		return
	}

	if err := h.cache.DeleteAPIKey(r.Context(), keyHash); err != nil {
		log.Error().Err(err).Str("hash", keyHash[:8]+"...").Msg("[internal] cache evict failed")
		ErrorJSON(w, http.StatusInternalServerError, "cache eviction failed")
		return
	}

	log.Info().Str("hash", keyHash[:8]+"...").Msg("[internal] api key evicted from cache")
	writeJSON(w, http.StatusOK, map[string]bool{"evicted": true})
}
