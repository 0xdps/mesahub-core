// Package handler — files.go handles /api/db/:name/files routes.
package handler

import (
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"github.com/0xdps/sqlite-hub/server/internal/auth"
	"github.com/0xdps/sqlite-hub/server/internal/cache"
	"github.com/0xdps/sqlite-hub/server/internal/config"
	"github.com/0xdps/sqlite-hub/server/internal/db"
	"github.com/0xdps/sqlite-hub/server/internal/files"
	"github.com/0xdps/sqlite-hub/server/internal/filetoken"
)

// FilesHandler holds dependencies for the files route group.
type FilesHandler struct {
	cfg      *config.Config
	registry *db.Registry
	storage  *files.Storage
	cache    cache.Client
}

// NewFilesHandler creates a FilesHandler.
func NewFilesHandler(cfg *config.Config, registry *db.Registry, storage *files.Storage, c cache.Client) *FilesHandler {
	return &FilesHandler{cfg: cfg, registry: registry, storage: storage, cache: c}
}

// List handles GET /api/db/:name/files.
func (h *FilesHandler) List(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	rec, err := h.registry.GetDatabase(name)
	if err != nil || rec == nil {
		ErrorJSON(w, http.StatusNotFound, "Database not found")
		return
	}

	// Auth: valid file access token OR normal DB auth.
	if !filetoken.ValidateFromRequest(r, name) {
		if code, msg := auth.AuthorizeDB(r, h.cfg, h.cache, rec); code != 0 {
			ErrorJSON(w, code, msg)
			return
		}
	}

	q := r.URL.Query()
	limit := queryInt(q, "limit", 100)
	if limit < 1 {
		limit = 1
	}
	if limit > 1000 {
		limit = 1000
	}
	offset := queryInt(q, "offset", 0)
	folderPrefix := q.Get("folder_prefix")

	result, err := h.storage.List(name, limit, offset, folderPrefix)
	if err != nil {
		ErrorJSON(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// Upload handles POST /api/db/:name/files.
func (h *FilesHandler) Upload(w http.ResponseWriter, r *http.Request) {
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

	if err := r.ParseMultipartForm(h.cfg.FileMaxSizeBytes + (1 << 20)); err != nil {
		ErrorJSON(w, http.StatusBadRequest, "invalid multipart form")
		return
	}

	file, fileHeader, err := r.FormFile("file")
	if err != nil {
		ErrorJSON(w, http.StatusBadRequest, "file field is required")
		return
	}
	defer file.Close()

	// Read file into memory (Storage.Upload takes []byte Data).
	data, err := io.ReadAll(io.LimitReader(file, h.cfg.FileMaxSizeBytes+1))
	if err != nil {
		ErrorJSON(w, http.StatusInternalServerError, "failed to read file")
		return
	}
	if int64(len(data)) > h.cfg.FileMaxSizeBytes {
		ErrorJSON(w, http.StatusRequestEntityTooLarge, files.ErrFileTooLarge.Error())
		return
	}

	filename := r.FormValue("filename")
	if filename == "" {
		filename = fileHeader.Filename
	}
	contentType := r.FormValue("content_type")
	if contentType == "" {
		contentType = fileHeader.Header.Get("Content-Type")
	}
	folderPath := r.FormValue("folder_path")
	conflictMode := files.ConflictMode(r.FormValue("conflict_mode"))
	metaStr := r.FormValue("metadata")
	expiresInStr := r.FormValue("expires_in")

	var expiresAt sql.NullString
	if expiresInStr != "" {
		if secs, err := strconv.Atoi(expiresInStr); err == nil && secs > 0 {
			// Use SQLite datetime expression.
			expiresAt = sql.NullString{
				String: fmt.Sprintf("datetime('now', '+%d seconds')", secs),
				Valid:  true,
			}
		}
	}

	in := files.UploadInput{
		DBName:       name,
		FolderPath:   folderPath,
		Filename:     filename,
		ContentType:  contentType,
		Data:         data,
		ConflictMode: conflictMode,
		ExpiresAt:    expiresAt,
		Metadata:     sql.NullString{String: metaStr, Valid: metaStr != ""},
	}

	result, err := h.storage.Upload(in)
	if err != nil {
		code := http.StatusInternalServerError
		switch err {
		case files.ErrFileTooLarge:
			code = http.StatusRequestEntityTooLarge
		case files.ErrConflict:
			code = http.StatusConflict
		case files.ErrTooManyFiles, files.ErrStorageQuota:
			code = http.StatusInsufficientStorage
		case files.ErrMimeNotAllowed, files.ErrMimeMismatch:
			code = http.StatusUnsupportedMediaType
		}
		ErrorJSON(w, code, err.Error())
		return
	}

	// Return the full file record so the caller gets url, uploaded_at etc.
	stored, _ := h.storage.GetByID(result.ID)
	if stored == nil {
		// Fallback to minimal upload result.
		writeJSON(w, http.StatusCreated, map[string]any{
			"id":         result.ID,
			"size_bytes": result.SizeBytes,
		})
		return
	}
	log.Info().Str("db", name).Str("file", stored.Filename).Msg("[files] uploaded")
	writeJSON(w, http.StatusCreated, fileRecord(stored, name, r))
}

// Download handles GET /api/db/:name/files/:id.
func (h *FilesHandler) Download(w http.ResponseWriter, r *http.Request) {
	h.serveFile(w, r, false)
}

// HeadFile handles HEAD /api/db/:name/files/:id.
func (h *FilesHandler) HeadFile(w http.ResponseWriter, r *http.Request) {
	h.serveFile(w, r, true)
}

func (h *FilesHandler) serveFile(w http.ResponseWriter, r *http.Request, headOnly bool) {
	name := chi.URLParam(r, "name")
	id := chi.URLParam(r, "id")

	rec, err := h.registry.GetDatabase(name)
	if err != nil || rec == nil {
		ErrorJSON(w, http.StatusNotFound, "Database not found")
		return
	}

	// Auth: file token OR DB auth.
	if !filetoken.ValidateFromRequest(r, name) {
		if code, msg := auth.AuthorizeDB(r, h.cfg, h.cache, rec); code != 0 {
			ErrorJSON(w, code, msg)
			return
		}
	}

	stored, err := h.storage.GetByID(id)
	if err != nil || stored == nil || stored.DBName != name {
		ErrorJSON(w, http.StatusNotFound, "File not found")
		return
	}

	ct := stored.ContentType.String
	if ct == "" {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Length", strconv.FormatInt(stored.SizeBytes, 10))
	w.Header().Set("Content-Disposition",
		"inline; filename=\""+strings.ReplaceAll(stored.Filename, `"`, `\"`)+`"`)

	if headOnly {
		w.WriteHeader(http.StatusOK)
		return
	}

	// X-Sendfile delivery — off-load to Caddy/nginx.
	if h.cfg.EnableFileProxyDelivery {
		w.Header().Set("X-Sendfile", stored.StoragePath)
		w.WriteHeader(http.StatusOK)
		return
	}

	// Stream blob directly.
	f, err := os.Open(stored.StoragePath)
	if err != nil {
		ErrorJSON(w, http.StatusInternalServerError, "file unavailable")
		return
	}
	defer f.Close()
	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(w, f); err != nil {
		log.Error().Err(err).Str("id", id).Msg("[files] stream error")
	}
}

// DeleteFile handles DELETE /api/db/:name/files/:id.
func (h *FilesHandler) DeleteFile(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	id := chi.URLParam(r, "id")

	rec, err := h.registry.GetDatabase(name)
	if err != nil || rec == nil {
		ErrorJSON(w, http.StatusNotFound, "Database not found")
		return
	}
	if code, msg := auth.AuthorizeDB(r, h.cfg, h.cache, rec); code != 0 {
		ErrorJSON(w, code, msg)
		return
	}

	if err := h.storage.DeleteByID(id, name); err != nil {
		if strings.Contains(err.Error(), "not found") {
			ErrorJSON(w, http.StatusNotFound, err.Error())
			return
		}
		ErrorJSON(w, http.StatusInternalServerError, err.Error())
		return
	}
	log.Info().Str("db", name).Str("id", id).Msg("[files] deleted")
	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// ── UUID-based handlers (control mode only) ───────────────────────────────────

// resolveUUID is a shared helper that looks up a UUID, fetches the registry
// record, and authorizes the request. Returns the template name and record on
// success; on failure it writes the error response and returns ("", nil).
func (h *FilesHandler) resolveUUID(w http.ResponseWriter, r *http.Request, uuid string) (string, *db.DBRecord) {
	templateName, _, err := auth.LookupByUUID(h.cfg.DataPath, uuid)
	if err != nil {
		ErrorJSON(w, http.StatusNotFound, "Database not found")
		return "", nil
	}
	rec, err := h.registry.GetDatabase(templateName)
	if err != nil || rec == nil {
		ErrorJSON(w, http.StatusNotFound, "Database not found")
		return "", nil
	}
	if code, msg := auth.AuthorizeDBByUUID(r, h.cfg, h.cache, rec, uuid); code != 0 {
		ErrorJSON(w, code, msg)
		return "", nil
	}
	return templateName, rec
}

// ListByUUID handles GET /api/files/:uuid.
func (h *FilesHandler) ListByUUID(w http.ResponseWriter, r *http.Request) {
	uuid := chi.URLParam(r, "uuid")
	templateName, _ := h.resolveUUID(w, r, uuid)
	if templateName == "" {
		return
	}

	q := r.URL.Query()
	limit := queryInt(q, "limit", 100)
	if limit < 1 {
		limit = 1
	}
	if limit > 1000 {
		limit = 1000
	}
	offset := queryInt(q, "offset", 0)
	folderPrefix := q.Get("folder_prefix")

	result, err := h.storage.List(templateName, limit, offset, folderPrefix)
	if err != nil {
		ErrorJSON(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Build file records with UUID-based download URLs.
	fileItems := make([]map[string]any, 0, len(result.Files))
	for i := range result.Files {
		fileItems = append(fileItems, fileRecordUUID(&result.Files[i], uuid))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"files":  fileItems,
		"total":  result.Total,
		"offset": result.Offset,
		"limit":  result.Limit,
	})
}

// UploadByUUID handles POST /api/files/:uuid.
func (h *FilesHandler) UploadByUUID(w http.ResponseWriter, r *http.Request) {
	uuid := chi.URLParam(r, "uuid")
	templateName, _ := h.resolveUUID(w, r, uuid)
	if templateName == "" {
		return
	}

	if err := r.ParseMultipartForm(h.cfg.FileMaxSizeBytes + (1 << 20)); err != nil {
		ErrorJSON(w, http.StatusBadRequest, "invalid multipart form")
		return
	}

	file, fileHeader, err := r.FormFile("file")
	if err != nil {
		ErrorJSON(w, http.StatusBadRequest, "file field is required")
		return
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, h.cfg.FileMaxSizeBytes+1))
	if err != nil {
		ErrorJSON(w, http.StatusInternalServerError, "failed to read file")
		return
	}
	if int64(len(data)) > h.cfg.FileMaxSizeBytes {
		ErrorJSON(w, http.StatusRequestEntityTooLarge, files.ErrFileTooLarge.Error())
		return
	}

	filename := r.FormValue("filename")
	if filename == "" {
		filename = fileHeader.Filename
	}
	contentType := r.FormValue("content_type")
	if contentType == "" {
		contentType = fileHeader.Header.Get("Content-Type")
	}
	folderPath := r.FormValue("folder_path")
	conflictMode := files.ConflictMode(r.FormValue("conflict_mode"))
	metaStr := r.FormValue("metadata")
	expiresInStr := r.FormValue("expires_in")

	var expiresAt sql.NullString
	if expiresInStr != "" {
		if secs, err := strconv.Atoi(expiresInStr); err == nil && secs > 0 {
			expiresAt = sql.NullString{
				String: fmt.Sprintf("datetime('now', '+%d seconds')", secs),
				Valid:  true,
			}
		}
	}

	in := files.UploadInput{
		DBName:       templateName,
		FolderPath:   folderPath,
		Filename:     filename,
		ContentType:  contentType,
		Data:         data,
		ConflictMode: conflictMode,
		ExpiresAt:    expiresAt,
		Metadata:     sql.NullString{String: metaStr, Valid: metaStr != ""},
	}

	result, err := h.storage.Upload(in)
	if err != nil {
		code := http.StatusInternalServerError
		switch err {
		case files.ErrFileTooLarge:
			code = http.StatusRequestEntityTooLarge
		case files.ErrConflict:
			code = http.StatusConflict
		case files.ErrTooManyFiles, files.ErrStorageQuota:
			code = http.StatusInsufficientStorage
		case files.ErrMimeNotAllowed, files.ErrMimeMismatch:
			code = http.StatusUnsupportedMediaType
		}
		ErrorJSON(w, code, err.Error())
		return
	}

	stored, _ := h.storage.GetByID(result.ID)
	if stored == nil {
		writeJSON(w, http.StatusCreated, map[string]any{
			"id":         result.ID,
			"size_bytes": result.SizeBytes,
		})
		return
	}
	log.Info().Str("db", templateName).Str("file", stored.Filename).Msg("[files] uploaded")
	writeJSON(w, http.StatusCreated, fileRecordUUID(stored, uuid))
}

// DownloadByUUID handles GET /api/files/:uuid/:id.
func (h *FilesHandler) DownloadByUUID(w http.ResponseWriter, r *http.Request) {
	h.serveFileByUUID(w, r, false)
}

// HeadFileByUUID handles HEAD /api/files/:uuid/:id.
func (h *FilesHandler) HeadFileByUUID(w http.ResponseWriter, r *http.Request) {
	h.serveFileByUUID(w, r, true)
}

func (h *FilesHandler) serveFileByUUID(w http.ResponseWriter, r *http.Request, headOnly bool) {
	uuid := chi.URLParam(r, "uuid")
	id := chi.URLParam(r, "id")

	templateName, _ := h.resolveUUID(w, r, uuid)
	if templateName == "" {
		return
	}

	stored, err := h.storage.GetByID(id)
	if err != nil || stored == nil || stored.DBName != templateName {
		ErrorJSON(w, http.StatusNotFound, "File not found")
		return
	}

	ct := stored.ContentType.String
	if ct == "" {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Length", strconv.FormatInt(stored.SizeBytes, 10))
	w.Header().Set("Content-Disposition",
		"inline; filename=\""+strings.ReplaceAll(stored.Filename, `"`, `\"`)+`"`)

	if headOnly {
		w.WriteHeader(http.StatusOK)
		return
	}

	if h.cfg.EnableFileProxyDelivery {
		w.Header().Set("X-Sendfile", stored.StoragePath)
		w.WriteHeader(http.StatusOK)
		return
	}

	f, err := os.Open(stored.StoragePath)
	if err != nil {
		ErrorJSON(w, http.StatusInternalServerError, "file unavailable")
		return
	}
	defer f.Close()
	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(w, f); err != nil {
		log.Error().Err(err).Str("id", id).Msg("[files] stream error")
	}
}

// DeleteFileByUUID handles DELETE /api/files/:uuid/:id.
func (h *FilesHandler) DeleteFileByUUID(w http.ResponseWriter, r *http.Request) {
	uuid := chi.URLParam(r, "uuid")
	id := chi.URLParam(r, "id")

	templateName, _ := h.resolveUUID(w, r, uuid)
	if templateName == "" {
		return
	}

	if err := h.storage.DeleteByID(id, templateName); err != nil {
		if strings.Contains(err.Error(), "not found") {
			ErrorJSON(w, http.StatusNotFound, err.Error())
			return
		}
		ErrorJSON(w, http.StatusInternalServerError, err.Error())
		return
	}
	log.Info().Str("db", templateName).Str("id", id).Msg("[files] deleted")
	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// fileRecordUUID converts a StoredFile into the wire-format map sent to clients
// via the UUID-based file routes (url uses /api/files/{uuid}/{id}).
func fileRecordUUID(f *files.StoredFile, uuid string) map[string]any {
	url := fmt.Sprintf("/api/files/%s/%s", uuid, f.ID)
	ct := ""
	if f.ContentType.Valid {
		ct = f.ContentType.String
	}
	return map[string]any{
		"id":           f.ID,
		"filename":     f.Filename,
		"folder_path":  f.FolderPath,
		"size_bytes":   f.SizeBytes,
		"content_type": ct,
		"url":          url,
		"uploaded_at":  f.UploadedAt,
		"expires_at":   nullStr(f.ExpiresAt),
		"metadata":     nullStr(f.Metadata),
	}
}

// fileRecord converts a StoredFile into the wire-format map sent to clients.
func fileRecord(f *files.StoredFile, dbName string, r *http.Request) map[string]any {
	url := fmt.Sprintf("/api/db/%s/files/%s", dbName, f.ID)
	ct := ""
	if f.ContentType.Valid {
		ct = f.ContentType.String
	}
	return map[string]any{
		"id":           f.ID,
		"filename":     f.Filename,
		"folder_path":  f.FolderPath,
		"size_bytes":   f.SizeBytes,
		"content_type": ct,
		"url":          url,
		"uploaded_at":  f.UploadedAt,
		"expires_at":   nullStr(f.ExpiresAt),
		"metadata":     nullStr(f.Metadata),
	}
}
