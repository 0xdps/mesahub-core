// Package control — databases.go handles control-plane database CRUD.
//
//	GET    /api/control/databases         — list user's databases
//	POST   /api/control/databases         — create database
//	DELETE /api/control/databases/{id}    — soft-delete database
package control

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"github.com/0xdps/sqlite-hub/server/internal/entitlements"
	"github.com/0xdps/sqlite-hub/server/internal/sysutil"
)

var displayNameRE = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9 _-]{0,62}$`)

// ListDatabases handles GET /api/control/databases.
func (h *Handler) ListDatabases(w http.ResponseWriter, r *http.Request) {
	user := UserFromCtx(r.Context())
	dbs, err := h.cdb.ListDatabases(user.ID)
	if err != nil {
		log.Error().Err(err).Msg("[control/db] ListDatabases")
		writeError(w, http.StatusInternalServerError, "failed to list databases")
		return
	}
	// Update size_bytes from filesystem for each db.
	for i, db := range dbs {
		sz := sysutil.FileSizeBytes(sysutil.DBPath(h.cfg.DataPath, db.Name))
		dbs[i].SizeBytes = sz
		if sz > 0 {
			_ = h.cdb.UpdateDatabaseSize(db.Name, sz)
		}
	}
	writeJSON(w, http.StatusOK, dbs)
}

// CreateDatabase handles POST /api/control/databases.
func (h *Handler) CreateDatabase(w http.ResponseWriter, r *http.Request) {
	user := UserFromCtx(r.Context())

	var body struct {
		DisplayName string `json:"display_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.DisplayName == "" {
		writeError(w, http.StatusBadRequest, "display_name is required")
		return
	}
	if !displayNameRE.MatchString(body.DisplayName) {
		writeError(w, http.StatusBadRequest, "display_name must be 1–63 chars, start with alphanumeric, contain only letters/digits/spaces/hyphens/underscores")
		return
	}

	// Check volume.
	vol := sysutil.VolumeInfo(h.cfg.DataPath)
	if vol.UsedPercent >= h.cfg.MaxVolumeUsagePct {
		writeError(w, http.StatusInsufficientStorage, "volume usage limit reached")
		return
	}

	// Enforce plan entitlements.
	plan := entitlements.Resolve(user.NubePlan, nil)
	if plan.MaxDatabases != -1 {
		count, err := h.cdb.CountActiveDatabases(user.ID)
		if err == nil && count >= plan.MaxDatabases {
			writeError(w, http.StatusForbidden, "database limit reached for plan")
			return
		}
	}

	// Derive the template-internal name: u<userID_prefix>_<slug>
	slug := slugify(body.DisplayName)
	prefix := user.ID
	if len(prefix) > 8 {
		prefix = prefix[:8]
	}
	name := "u" + strings.ToLower(prefix) + "_" + slug

	db, err := h.cdb.CreateDatabase(user.ID, name, body.DisplayName)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint") {
			writeError(w, http.StatusConflict, "a database with that name already exists")
			return
		}
		log.Error().Err(err).Msg("[control/db] CreateDatabase")
		writeError(w, http.StatusInternalServerError, "failed to create database")
		return
	}

	// Touch the SQLite file (creates it if absent) and register in the
	// template-level registry so /api/query/:uuid can serve it.
	if _, fileErr := h.pool.Get(name); fileErr != nil {
		log.Error().Err(fileErr).Str("db", name).Msg("[control/db] create db file")
		writeError(w, http.StatusInternalServerError, "failed to create database file")
		return
	}
	existing, _ := h.registry.GetDatabase(name)
	if existing == nil {
		if _, regErr := h.registry.InsertDatabase(name, user.ID, nil, nil); regErr != nil {
			log.Error().Err(regErr).Str("db", name).Msg("[control/db] registry insert")
		}
	}

	log.Info().Str("user", user.Email).Str("db", name).Msg("[control/db] database created")
	writeJSON(w, http.StatusCreated, db)
}

// DeleteDatabase handles DELETE /api/control/databases/{id}.
func (h *Handler) DeleteDatabase(w http.ResponseWriter, r *http.Request) {
	user := UserFromCtx(r.Context())
	id := chi.URLParam(r, "id")

	dbRec, lookupErr := h.cdb.GetDatabase(id)
	if err := h.cdb.SoftDeleteDatabase(id, user.ID); err != nil {
		log.Warn().Err(err).Str("id", id).Msg("[control/db] DeleteDatabase")
		writeError(w, http.StatusNotFound, "database not found or not owned by you")
		return
	}

	// Mirror status in registry so /api/query/:uuid returns 503 for deleted DBs.
	if lookupErr == nil && dbRec != nil {
		_ = h.registry.SetDatabaseStatus(dbRec.Name, "inactive")
	}

	log.Info().Str("user", user.Email).Str("id", id).Msg("[control/db] database deleted")
	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// slugify converts a display name to a URL-safe slug.
func slugify(s string) string {
	s = strings.ToLower(s)
	s = regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(s, "_")
	s = strings.Trim(s, "_")
	if len(s) > 32 {
		s = s[:32]
	}
	return s
}
