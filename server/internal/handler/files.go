// Package handler — files.go handles /api/db/:name/files routes.
package handler

import (
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

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

	// Auth: valid (non-revoked) file access token OR normal DB auth.
	if !filetoken.ValidateFromRequest(r, name, func(tokenID string) bool {
		revoked, _ := h.registry.IsFileTokenRevoked(tokenID, name)
		return revoked
	}) {
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
	sort := q.Get("sort")
	order := q.Get("order")

	result, err := h.storage.List(name, limit, offset, folderPrefix, sort, order)
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

	// Auth: valid (non-revoked) file token OR DB auth.
	if !filetoken.ValidateFromRequest(r, name, func(tokenID string) bool {
		revoked, _ := h.registry.IsFileTokenRevoked(tokenID, name)
		return revoked
	}) {
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
	// Must be a relative path (filename only): Caddy resolves it against the
	// configured root (/data/files/blobs). An absolute path doubles the prefix.
	if h.cfg.EnableFileProxyDelivery {
		w.Header().Set("X-Sendfile", "/"+filepath.Base(stored.StoragePath))
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

// Meta handles GET /api/db/:name/files/:id/meta
// Returns file metadata only — no blob data is returned.
func (h *FilesHandler) Meta(w http.ResponseWriter, r *http.Request) {
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

	stored, err := h.storage.GetByID(id)
	if err != nil || stored == nil || stored.DBName != name {
		ErrorJSON(w, http.StatusNotFound, "File not found")
		return
	}
	writeJSON(w, http.StatusOK, fileRecord(stored, name, r))
}

// PresignFile handles POST /api/db/:name/files/:id/presign
// Creates a time-limited HMAC presigned download URL for the file.
func (h *FilesHandler) PresignFile(w http.ResponseWriter, r *http.Request) {
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

	stored, err := h.storage.GetByID(id)
	if err != nil || stored == nil || stored.DBName != name {
		ErrorJSON(w, http.StatusNotFound, "File not found")
		return
	}

	var body struct {
		ExpiresIn   int    `json:"expires_in"`
		Disposition string `json:"disposition"` // "inline" or "attachment"
	}
	_ = decodeJSONOpt(r, &body)

	result, err := filetoken.Create(name, body.ExpiresIn)
	if err != nil {
		ErrorJSON(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Build the presigned URL pointing at the shortlink route on the API server.
	scheme := "https"
	if r.TLS == nil {
		scheme = "http"
	}
	host := r.Host
	disp := body.Disposition
	if disp != "attachment" && disp != "inline" {
		disp = "inline"
	}
	presignedURL := fmt.Sprintf("%s://%s/%s/file/%s?token=%s&dis=%s",
		scheme, host, name, id, result.Token, disp)

	log.Info().Str("db", name).Str("file", id).Msg("[files] presigned")
	writeJSON(w, http.StatusOK, map[string]any{
		"url":        presignedURL,
		"token_id":   result.TokenID,
		"expires_at": result.ExpiresAt.UTC().Format(time.RFC3339),
		"expires_in": result.ExpiresIn,
	})
}

// PresignBatch handles POST /api/db/:name/files/presign/batch
// Batch-presigns up to FileBulkDeleteMaxIDs file IDs.
func (h *FilesHandler) PresignBatch(w http.ResponseWriter, r *http.Request) {
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
		IDs       []string `json:"ids"`
		ExpiresIn int      `json:"expires_in"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if len(body.IDs) == 0 {
		ErrorJSON(w, http.StatusBadRequest, "ids must be a non-empty array")
		return
	}
	maxIDs := h.cfg.FileBulkDeleteMaxIDs
	if maxIDs <= 0 {
		maxIDs = 100
	}
	if len(body.IDs) > maxIDs {
		ErrorJSON(w, http.StatusBadRequest, fmt.Sprintf("too many ids — maximum is %d", maxIDs))
		return
	}

	scheme := "https"
	if r.TLS == nil {
		scheme = "http"
	}
	host := r.Host

	type result struct {
		ID  string `json:"id"`
		URL string `json:"url"`
	}
	results := make([]result, 0, len(body.IDs))
	for _, fileID := range body.IDs {
		tok, err := filetoken.Create(name, body.ExpiresIn)
		if err != nil {
			ErrorJSON(w, http.StatusInternalServerError, err.Error())
			return
		}
		u := fmt.Sprintf("%s://%s/%s/file/%s?token=%s", scheme, host, name, fileID, tok.Token)
		results = append(results, result{ID: fileID, URL: u})
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

// BulkDeleteFiles handles POST /api/db/:name/files/bulk-delete
// Deletes multiple files in a single call.
func (h *FilesHandler) BulkDeleteFiles(w http.ResponseWriter, r *http.Request) {
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
		IDs []string `json:"ids"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if len(body.IDs) == 0 {
		ErrorJSON(w, http.StatusBadRequest, "ids must be a non-empty array")
		return
	}
	maxIDs := h.cfg.FileBulkDeleteMaxIDs
	if maxIDs <= 0 {
		maxIDs = 100
	}
	if len(body.IDs) > maxIDs {
		ErrorJSON(w, http.StatusBadRequest, fmt.Sprintf("too many ids — maximum is %d", maxIDs))
		return
	}

	var deleted, failed int
	for _, id := range body.IDs {
		if err := h.storage.DeleteByID(id, name); err != nil {
			failed++
		} else {
			deleted++
		}
	}
	log.Info().Str("db", name).Int("deleted", deleted).Int("failed", failed).Msg("[files] bulk delete")
	writeJSON(w, http.StatusOK, map[string]any{
		"deleted": deleted,
		"failed":  failed,
		"success": failed == 0,
	})
}

// FileShortlink handles GET /{dbName}/file/{fileId}
// Serves a file via X-Sendfile using a presigned file-access token.
// No admin session required — only a valid file access token.
func (h *FilesHandler) FileShortlink(w http.ResponseWriter, r *http.Request) {
	dbName := chi.URLParam(r, "dbName")
	id := chi.URLParam(r, "id")

	if !filetoken.ValidateFromRequest(r, dbName, func(tokenID string) bool {
		revoked, _ := h.registry.IsFileTokenRevoked(tokenID, dbName)
		return revoked
	}) {
		ErrorJSON(w, http.StatusUnauthorized, "Missing or invalid file access token")
		return
	}

	stored, err := h.storage.GetByID(id)
	if err != nil || stored == nil || stored.DBName != dbName {
		ErrorJSON(w, http.StatusNotFound, "File not found")
		return
	}

	// Honour expires_at — return 410 Gone if the file record has expired.
	if stored.ExpiresAt.Valid && stored.ExpiresAt.String != "" {
		expAt, parseErr := time.Parse("2006-01-02 15:04:05", stored.ExpiresAt.String)
		if parseErr == nil && time.Now().After(expAt) {
			ErrorJSON(w, http.StatusGone, "File has expired")
			return
		}
	}

	ct := stored.ContentType.String
	if ct == "" {
		ct = "application/octet-stream"
	}
	disp := r.URL.Query().Get("dis")
	if disp != "attachment" && disp != "inline" {
		disp = "inline"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Length", strconv.FormatInt(stored.SizeBytes, 10))
	w.Header().Set("Content-Disposition",
		disp+"; filename=\""+strings.ReplaceAll(stored.Filename, `"`, `\"`)+`"`)

	if h.cfg.EnableFileProxyDelivery {
		w.Header().Set("X-Sendfile", "/"+filepath.Base(stored.StoragePath))
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
	_, _ = io.Copy(w, f)
}

// resolveRef is a shared helper that resolves a ref (UUID or template name),
// fetches the registry record, and authorizes the request. Returns the template
// name and record on success; on failure it writes the error response and returns ("", nil).
func (h *FilesHandler) resolveRef(w http.ResponseWriter, r *http.Request, ref string) (string, *db.DBRecord) {
	templName, _, resolvedUUID, err := auth.ResolveDB(h.cfg.DataPath, ref)
	if err != nil {
		ErrorJSON(w, http.StatusNotFound, "Database not found")
		return "", nil
	}
	rec, err := h.registry.GetDatabase(templName)
	if err != nil || rec == nil {
		ErrorJSON(w, http.StatusNotFound, "Database not found")
		return "", nil
	}
	if code, msg := auth.AuthorizeDBByUUID(r, h.cfg, h.cache, rec, resolvedUUID); code != 0 {
		ErrorJSON(w, code, msg)
		return "", nil
	}
	return templName, rec
}

// ListByUUID handles GET /api/files/:ref.
func (h *FilesHandler) ListByUUID(w http.ResponseWriter, r *http.Request) {
	ref := chi.URLParam(r, "ref")
	templateName, _ := h.resolveRef(w, r, ref)
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
	sort := q.Get("sort")
	order := q.Get("order")

	result, err := h.storage.List(templateName, limit, offset, folderPrefix, sort, order)
	if err != nil {
		ErrorJSON(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Build file records with UUID-based download URLs.
	fileItems := make([]map[string]any, 0, len(result.Files))
	for i := range result.Files {
		fileItems = append(fileItems, fileRecordUUID(&result.Files[i], ref))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"files":  fileItems,
		"total":  result.Total,
		"offset": result.Offset,
		"limit":  result.Limit,
	})
}

// UploadByUUID handles POST /api/files/:ref.
func (h *FilesHandler) UploadByUUID(w http.ResponseWriter, r *http.Request) {
	ref := chi.URLParam(r, "ref")
	templateName, _ := h.resolveRef(w, r, ref)
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
	writeJSON(w, http.StatusCreated, fileRecordUUID(stored, ref))
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
	ref := chi.URLParam(r, "ref")
	id := chi.URLParam(r, "id")

	templateName, _ := h.resolveRef(w, r, ref)
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

// DeleteFileByUUID handles DELETE /api/files/:ref/:id.
func (h *FilesHandler) DeleteFileByUUID(w http.ResponseWriter, r *http.Request) {
	ref := chi.URLParam(r, "ref")
	id := chi.URLParam(r, "id")

	templateName, _ := h.resolveRef(w, r, ref)
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
// via the ref-based file routes (url uses /api/files/{ref}/{id}).
func fileRecordUUID(f *files.StoredFile, ref string) map[string]any {
	url := fmt.Sprintf("/api/files/%s/%s", ref, f.ID)
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
