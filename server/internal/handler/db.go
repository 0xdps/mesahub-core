// Package handler — db.go handles /api/db and /api/db/:name routes.
package handler

import (
	"net/http"
	"os"
	"regexp"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"github.com/0xdps/sqlite-hub/server/internal/auth"
	"github.com/0xdps/sqlite-hub/server/internal/config"
	"github.com/0xdps/sqlite-hub/server/internal/db"
	"github.com/0xdps/sqlite-hub/server/internal/sysutil"
)

var nameRegex = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// DBHandler holds dependencies for the /api/db route group.
type DBHandler struct {
	cfg      *config.Config
	pool     *db.Pool
	registry *db.Registry
}

// NewDBHandler creates a DBHandler.
func NewDBHandler(cfg *config.Config, pool *db.Pool, registry *db.Registry) *DBHandler {
	return &DBHandler{cfg: cfg, pool: pool, registry: registry}
}

// ── /api/db ───────────────────────────────────────────────────────────────────

// ListDBs handles GET /api/db (admin only).
func (h *DBHandler) ListDBs(w http.ResponseWriter, r *http.Request) {
	rows, err := h.registry.ListDatabases()
	if err != nil {
		ErrorJSON(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]any, len(rows))
	for i, rec := range rows {
		out[i] = h.withStats(&rec)
	}
	writeJSON(w, http.StatusOK, out)
}

// CreateDB handles POST /api/db (admin only).
func (h *DBHandler) CreateDB(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name        string `json:"name"`
		Owner       string `json:"owner"`
		Description string `json:"description"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.Name == "" || body.Owner == "" {
		ErrorJSON(w, http.StatusBadRequest, "name and owner are required")
		return
	}
	if !nameRegex.MatchString(body.Name) {
		ErrorJSON(w, http.StatusBadRequest, "name must match ^[A-Za-z0-9_-]+$")
		return
	}

	pct := sysutil.VolumeUsagePct(h.cfg.DataPath)
	if pct >= h.cfg.MaxVolumeUsagePct {
		ErrorJSON(w, http.StatusInsufficientStorage,
			"volume usage "+itoa(pct)+"% exceeds limit of "+itoa(h.cfg.MaxVolumeUsagePct)+"%")
		return
	}

	existing, _ := h.registry.GetDatabase(body.Name)
	if existing != nil {
		ErrorJSON(w, http.StatusConflict, "Database already exists")
		return
	}

	// Touch the SQLite file with WAL mode via the pool (creates if absent).
	if _, err := h.pool.Get(body.Name); err != nil {
		ErrorJSON(w, http.StatusInternalServerError, "failed to create database file: "+err.Error())
		return
	}

	var desc *string
	if body.Description != "" {
		d := body.Description
		desc = &d
	}

	record, err := h.registry.InsertDatabase(body.Name, body.Owner, desc)
	if err != nil {
		ErrorJSON(w, http.StatusInternalServerError, err.Error())
		return
	}
	log.Info().Str("name", body.Name).Str("owner", body.Owner).Msg("[db] created database")

	writeJSON(w, http.StatusCreated, h.withStats(record))
}

// ── /api/db/:name ─────────────────────────────────────────────────────────────

// GetDB handles GET /api/db/:name.
func (h *DBHandler) GetDB(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	record, err := h.registry.GetDatabase(name)
	if err != nil || record == nil {
		ErrorJSON(w, http.StatusNotFound, "Not found")
		return
	}
	writeJSON(w, http.StatusOK, h.withStats(record))
}

// PatchDB handles PATCH /api/db/:name (admin only).
func (h *DBHandler) PatchDB(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	record, err := h.registry.GetDatabase(name)
	if err != nil || record == nil {
		ErrorJSON(w, http.StatusNotFound, "Not found")
		return
	}

	var body map[string]any
	if !decodeJSON(w, r, &body) {
		return
	}
	action, _ := body["action"].(string)

	switch action {
	case "set_status":
		status, _ := body["status"].(string)
		if status != "active" && status != "inactive" {
			ErrorJSON(w, http.StatusBadRequest, "status must be 'active' or 'inactive'")
			return
		}
		if err := h.registry.SetDatabaseStatus(name, status); err != nil {
			ErrorJSON(w, http.StatusInternalServerError, err.Error())
			return
		}
		log.Info().Str("name", name).Str("status", status).Msg("[db] status set")
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "status": status})

	case "reset_db":
		dbPath := sysutil.DBPath(h.cfg.DataPath, name)
		if _, err := os.Stat(dbPath); os.IsNotExist(err) {
			ErrorJSON(w, http.StatusNotFound, "Database file not found")
			return
		}
		if err := h.pool.Remove(name); err != nil {
			log.Warn().Err(err).Str("name", name).Msg("[db] pool remove error on reset")
		}
		// Wipe the database file and WAL/SHM sidecars so the DB is truly empty.
		_ = os.Remove(dbPath)
		_ = os.Remove(dbPath + "-wal")
		_ = os.Remove(dbPath + "-shm")
		log.Info().Str("name", name).Msg("[db] database reset — file deleted")
		writeJSON(w, http.StatusOK, map[string]bool{"success": true})

	default:
		ErrorJSON(w, http.StatusBadRequest, "Invalid action")
	}
}

// DeleteDB handles DELETE /api/db/:name (admin only, soft delete).
func (h *DBHandler) DeleteDB(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	record, err := h.registry.GetDatabase(name)
	if err != nil || record == nil {
		ErrorJSON(w, http.StatusNotFound, "Not found")
		return
	}
	if err := h.registry.SoftDeleteDatabase(h.pool, name); err != nil {
		ErrorJSON(w, http.StatusInternalServerError, err.Error())
		return
	}
	log.Info().Str("name", name).Msg("[db] soft deleted")
	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// ── /api/db/deleted ───────────────────────────────────────────────────────────

// ListDeletedDBs handles GET /api/db/deleted (admin only).
func (h *DBHandler) ListDeletedDBs(w http.ResponseWriter, r *http.Request) {
	rows, err := h.registry.ListDeletedDatabases()
	if err != nil {
		ErrorJSON(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]any, len(rows))
	for i, rec := range rows {
		out[i] = h.withStats(&rec)
	}
	writeJSON(w, http.StatusOK, out)
}

// DeletedDBAction handles POST /api/db/deleted/:name (restore or hard_delete).
func (h *DBHandler) DeletedDBAction(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	record, err := h.registry.GetDatabase(name)
	if err != nil || record == nil || record.Status != "deleted" {
		ErrorJSON(w, http.StatusNotFound, "Deleted database not found")
		return
	}

	var body map[string]any
	if !decodeJSON(w, r, &body) {
		return
	}
	action, _ := body["action"].(string)

	switch action {
	case "restore":
		if !record.OriginalName.Valid {
			ErrorJSON(w, http.StatusBadRequest, "Missing original name")
			return
		}
		existing, _ := h.registry.GetDatabase(record.OriginalName.String)
		if existing != nil {
			ErrorJSON(w, http.StatusConflict,
				"A database named \""+record.OriginalName.String+"\" already exists")
			return
		}
		restored, err := h.registry.RestoreDatabase(h.pool, name)
		if err != nil {
			ErrorJSON(w, http.StatusBadRequest, err.Error())
			return
		}
		log.Info().Str("from", name).Str("to", restored.Name).Msg("[db] restored")
		writeJSON(w, http.StatusOK, h.withStats(restored))

	case "hard_delete":
		if err := h.registry.HardDeleteDatabase(name); err != nil {
			ErrorJSON(w, http.StatusBadRequest, err.Error())
			return
		}
		log.Info().Str("name", name).Msg("[db] hard deleted")
		writeJSON(w, http.StatusOK, map[string]bool{"success": true})

	default:
		ErrorJSON(w, http.StatusBadRequest, "Invalid action. Use 'restore' or 'hard_delete'")
	}
}

// RequireAdmin is a convenience alias so routes in main.go avoid a second import.
func RequireAdmin(next http.Handler) http.Handler {
	return auth.RequireAdmin(next)
}

// ── helpers ───────────────────────────────────────────────────────────────────

func (h *DBHandler) withStats(rec *db.DBRecord) map[string]any {
	filePath := sysutil.DBPath(h.cfg.DataPath, rec.Name)
	_, statErr := os.Stat(filePath)
	return map[string]any{
		"id":            rec.ID,
		"name":          rec.Name,
		"owner":         rec.Owner,
		"description":   nullStr(rec.Description),
		"created_at":    rec.CreatedAt,
		"status":        rec.Status,
		"original_name": nullStr(rec.OriginalName),
		"deleted_at":    nullStr(rec.DeletedAt),
		"file_path":     filePath,
		"size_bytes":    sysutil.FileSizeBytes(filePath),
		"exists":        statErr == nil,
	}
}
