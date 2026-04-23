// Package handler — maintenance.go handles POST /api/maintenance/cleanup.
package handler

import (
	"net/http"

	"github.com/rs/zerolog/log"

	"github.com/0xdps/mesahub-core/config"
	"github.com/0xdps/mesahub-core/db"
	"github.com/0xdps/mesahub-core/files"
)

// MaintenanceHandler holds deps for maintenance routes.
type MaintenanceHandler struct {
	cfg      *config.Config
	registry *db.Registry
	storage  *files.Storage
}

// NewMaintenanceHandler creates a MaintenanceHandler.
func NewMaintenanceHandler(cfg *config.Config, registry *db.Registry, storage *files.Storage) *MaintenanceHandler {
	return &MaintenanceHandler{cfg: cfg, registry: registry, storage: storage}
}

// Cleanup handles POST /api/maintenance/cleanup (admin only).
func (h *MaintenanceHandler) Cleanup(w http.ResponseWriter, r *http.Request) {
	// Body is optional.
	_ = decodeJSONOpt(r, &struct{}{})

	if err := h.storage.CleanupExpired(); err != nil {
		ErrorJSON(w, http.StatusInternalServerError, "file cleanup failed: "+err.Error())
		return
	}

	tokenRevocationsDeleted, err := h.registry.CleanupExpiredTokenRevocations()
	if err != nil {
		ErrorJSON(w, http.StatusInternalServerError, "token cleanup failed: "+err.Error())
		return
	}

	// Best-effort audit logging.
	if auditErr := h.registry.RecordAuditEvent("maintenance_cleanup", nil, nil, map[string]any{
		"token_revocations_deleted": tokenRevocationsDeleted,
	}); auditErr != nil {
		log.Warn().Err(auditErr).Msg("[maintenance] audit insert failed")
	}

	log.Info().
		Int64("token_revocations_deleted", tokenRevocationsDeleted).
		Msg("[maintenance] cleanup complete")

	writeJSON(w, http.StatusOK, map[string]any{
		"files":  map[string]string{"status": "ok"},
		"tokens": map[string]any{"revocations_deleted": tokenRevocationsDeleted},
	})
}
