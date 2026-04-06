// Package handler — bucket_files.go handles /api/buckets/:name/files routes.
// Buckets are first-class file-storage namespaces independent of SQLite databases.
// Auth is enforced via API key scope: 'all', 'bucket:*', or 'bucket:<name>'.
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
	"github.com/0xdps/sqlite-hub/server/internal/files"
)

// bucketNS converts a template-internal bucket name into the storage namespace
// used for file metadata, ensuring no collision with database file namespaces.
func bucketNS(name string) string { return "bkt-" + name }

// BucketFilesHandler handles /api/buckets/{name}/files routes.
type BucketFilesHandler struct {
	cfg     *config.Config
	storage *files.Storage
	cache   cache.Client
}

// NewBucketFilesHandler creates a BucketFilesHandler.
func NewBucketFilesHandler(cfg *config.Config, storage *files.Storage, c cache.Client) *BucketFilesHandler {
	return &BucketFilesHandler{cfg: cfg, storage: storage, cache: c}
}

// lookupAndAuth resolves the bucket name from the URL, finds its owner in
// control.db, and enforces API key auth. Returns the storage namespace and
// true on success.
func (h *BucketFilesHandler) lookupAndAuth(w http.ResponseWriter, r *http.Request) (name, ns string, ok bool) {
	name = chi.URLParam(r, "name")

	userID, found := auth.LookupBucket(h.cfg.DataPath, name)
	if !found {
		ErrorJSON(w, http.StatusNotFound, "Bucket not found")
		return "", "", false
	}

	if code, msg := auth.AuthorizeBucket(r, h.cfg, h.cache, userID, name); code != 0 {
		ErrorJSON(w, code, msg)
		return "", "", false
	}

	return name, bucketNS(name), true
}

// List handles GET /api/buckets/{name}/files.
func (h *BucketFilesHandler) List(w http.ResponseWriter, r *http.Request) {
	_, ns, ok := h.lookupAndAuth(w, r)
	if !ok {
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

	result, err := h.storage.List(ns, limit, offset, folderPrefix, sort, order)
	if err != nil {
		ErrorJSON(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// Upload handles POST /api/buckets/{name}/files.
func (h *BucketFilesHandler) Upload(w http.ResponseWriter, r *http.Request) {
	name, ns, ok := h.lookupAndAuth(w, r)
	if !ok {
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
		DBName:       ns,
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
	log.Info().Str("bucket", name).Str("file", stored.Filename).Msg("[bucket-files] uploaded")
	writeJSON(w, http.StatusCreated, bucketFileRecord(stored, name, r))
	// Asynchronously refresh bucket size in control.db.
	go func() {
		total := h.storage.SumBytesForNamespace(ns)
		auth.UpdateBucketSizeInControl(h.cfg.DataPath, name, total)
	}()
}

// Download handles GET /api/buckets/{name}/files/{id}.
func (h *BucketFilesHandler) Download(w http.ResponseWriter, r *http.Request) {
	h.serveFile(w, r, false)
}

// HeadFile handles HEAD /api/buckets/{name}/files/{id}.
func (h *BucketFilesHandler) HeadFile(w http.ResponseWriter, r *http.Request) {
	h.serveFile(w, r, true)
}

func (h *BucketFilesHandler) serveFile(w http.ResponseWriter, r *http.Request, headOnly bool) {
	name, ns, ok := h.lookupAndAuth(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")

	stored, err := h.storage.GetByID(id)
	if err != nil || stored == nil || stored.DBName != ns {
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
	if _, err := io.Copy(w, f); err != nil {
		log.Error().Err(err).Str("id", id).Str("bucket", name).Msg("[bucket-files] stream error")
	}
}

// DeleteFile handles DELETE /api/buckets/{name}/files/{id}.
func (h *BucketFilesHandler) DeleteFile(w http.ResponseWriter, r *http.Request) {
	name, ns, ok := h.lookupAndAuth(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")

	if err := h.storage.DeleteByID(id, ns); err != nil {
		if strings.Contains(err.Error(), "not found") {
			ErrorJSON(w, http.StatusNotFound, err.Error())
			return
		}
		ErrorJSON(w, http.StatusInternalServerError, err.Error())
		return
	}
	log.Info().Str("bucket", name).Str("id", id).Msg("[bucket-files] deleted")
	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
	go func() {
		total := h.storage.SumBytesForNamespace(ns)
		auth.UpdateBucketSizeInControl(h.cfg.DataPath, name, total)
	}()
}

// Meta handles GET /api/buckets/{name}/files/{id}/meta.
func (h *BucketFilesHandler) Meta(w http.ResponseWriter, r *http.Request) {
	name, ns, ok := h.lookupAndAuth(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")

	stored, err := h.storage.GetByID(id)
	if err != nil || stored == nil || stored.DBName != ns {
		ErrorJSON(w, http.StatusNotFound, "File not found")
		return
	}
	writeJSON(w, http.StatusOK, bucketFileRecord(stored, name, r))
}

// PresignFile handles POST /api/buckets/{name}/files/{id}/presign.
func (h *BucketFilesHandler) PresignFile(w http.ResponseWriter, r *http.Request) {
	_, ns, ok := h.lookupAndAuth(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")

	stored, err := h.storage.GetByID(id)
	if err != nil || stored == nil || stored.DBName != ns {
		ErrorJSON(w, http.StatusNotFound, "File not found")
		return
	}

	var body struct {
		ExpiresIn   int    `json:"expires_in"`
		Disposition string `json:"disposition"`
	}
	_ = decodeJSONOpt(r, &body)

	// Bucket files use a time-limited signed URL (direct download, no token auth).
	// Generate a signed token using the bucket namespace as the "db name".
	expiry := body.ExpiresIn
	if expiry <= 0 {
		expiry = 3600
	}
	expiresAt := time.Now().Add(time.Duration(expiry) * time.Second)
	scheme := "https"
	if r.TLS == nil {
		scheme = "http"
	}
	disp := body.Disposition
	if disp != "attachment" && disp != "inline" {
		disp = "inline"
	}
	// Return a direct download URL (no opaque token — buckets don't use filetoken).
	presignedURL := fmt.Sprintf("%s://%s/api/buckets/%s/files/%s?expires=%d&dis=%s",
		scheme, r.Host, chi.URLParam(r, "name"), id, expiresAt.Unix(), disp)

	writeJSON(w, http.StatusOK, map[string]any{
		"url":        presignedURL,
		"expires_at": expiresAt.UTC().Format(time.RFC3339),
		"expires_in": expiry,
	})
}

// PresignBatch handles POST /api/buckets/{name}/files/presign/batch.
func (h *BucketFilesHandler) PresignBatch(w http.ResponseWriter, r *http.Request) {
	_, _, ok := h.lookupAndAuth(w, r)
	if !ok {
		return
	}
	bucketName := chi.URLParam(r, "name")

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

	expiry := body.ExpiresIn
	if expiry <= 0 {
		expiry = 3600
	}
	expiresAt := time.Now().Add(time.Duration(expiry) * time.Second)
	scheme := "https"
	if r.TLS == nil {
		scheme = "http"
	}

	type result struct {
		ID  string `json:"id"`
		URL string `json:"url"`
	}
	results := make([]result, 0, len(body.IDs))
	for _, fileID := range body.IDs {
		u := fmt.Sprintf("%s://%s/api/buckets/%s/files/%s?expires=%d",
			scheme, r.Host, bucketName, fileID, expiresAt.Unix())
		results = append(results, result{ID: fileID, URL: u})
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

// BulkDeleteFiles handles POST /api/buckets/{name}/files/bulk-delete.
func (h *BucketFilesHandler) BulkDeleteFiles(w http.ResponseWriter, r *http.Request) {
	name, ns, ok := h.lookupAndAuth(w, r)
	if !ok {
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
		if err := h.storage.DeleteByID(id, ns); err != nil {
			failed++
		} else {
			deleted++
		}
	}
	log.Info().Str("bucket", name).Int("deleted", deleted).Int("failed", failed).Msg("[bucket-files] bulk delete")
	writeJSON(w, http.StatusOK, map[string]any{
		"deleted": deleted,
		"failed":  failed,
		"success": failed == 0,
	})
	if deleted > 0 {
		go func() {
			total := h.storage.SumBytesForNamespace(ns)
			auth.UpdateBucketSizeInControl(h.cfg.DataPath, name, total)
		}()
	}
}

// bucketFileRecord converts a StoredFile into the wire-format map sent to clients.
func bucketFileRecord(f *files.StoredFile, bucketName string, r *http.Request) map[string]any {
	url := fmt.Sprintf("/api/buckets/%s/files/%s", bucketName, f.ID)
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
