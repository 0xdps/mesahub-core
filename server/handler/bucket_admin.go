// Package handler — bucket_admin.go handles admin-level /api/buckets routes.
// These are backed by registry.db (local volume buckets, shk_ + admin auth).
// SaaS bucket file operations (shs_ auth, S3/R2 backend) live in the saas module.
package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/0xdps/sqlite-hub-template/db"
)

// BucketAdminHandler handles admin-level /api/buckets routes.
type BucketAdminHandler struct {
	registry *db.Registry
}

// NewBucketAdminHandler creates a BucketAdminHandler.
func NewBucketAdminHandler(registry *db.Registry) *BucketAdminHandler {
	return &BucketAdminHandler{registry: registry}
}

// ListBuckets handles GET /api/buckets (admin only).
func (h *BucketAdminHandler) ListBuckets(w http.ResponseWriter, r *http.Request) {
	buckets, err := h.registry.ListBuckets()
	if err != nil {
		ErrorJSON(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]map[string]any, len(buckets))
	for i, b := range buckets {
		out[i] = map[string]any{
			"id":           b.ID,
			"name":         b.Name,
			"display_name": b.DisplayName,
			"description":  nullableString(b.Description),
			"status":       b.Status,
			"size_bytes":   b.SizeBytes,
			"created_at":   b.CreatedAt,
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// CreateBucket handles POST /api/buckets (admin only).
func (h *BucketAdminHandler) CreateBucket(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name        string  `json:"name"`
		DisplayName string  `json:"display_name"`
		Owner       string  `json:"owner"`
		Source      string  `json:"source"`
		InstanceID  *string `json:"instance_id"`
		Description *string `json:"description"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.Name == "" || body.DisplayName == "" {
		ErrorJSON(w, http.StatusBadRequest, "name and display_name are required")
		return
	}
	if body.Owner == "" {
		body.Owner = "admin"
	}
	if body.Source == "" {
		body.Source = "admin"
	}
	if !nameRegex.MatchString(body.Name) {
		ErrorJSON(w, http.StatusBadRequest, "name may only contain letters, digits, hyphens, and underscores")
		return
	}

	id := uuid.New().String()
	rec, err := h.registry.InsertBucket(id, body.Name, body.DisplayName, body.Owner, body.Source, body.InstanceID, body.Description)
	if err != nil {
		ErrorJSON(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":           rec.ID,
		"name":         rec.Name,
		"display_name": rec.DisplayName,
		"description":  nullableString(rec.Description),
		"status":       rec.Status,
		"size_bytes":   rec.SizeBytes,
		"created_at":   rec.CreatedAt,
	})
}

// DeleteBucket handles DELETE /api/buckets/{name} (admin only).
func (h *BucketAdminHandler) DeleteBucket(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "" {
		ErrorJSON(w, http.StatusBadRequest, "name is required")
		return
	}
	if err := h.registry.DeleteBucket(name); err != nil {
		ErrorJSON(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
