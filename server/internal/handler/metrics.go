// Package handler — metrics.go handles GET /api/metrics.
package handler

import (
	"net/http"
	"path/filepath"

	"github.com/0xdps/sqlite-hub/server/internal/config"
	"github.com/0xdps/sqlite-hub/server/internal/db"
	"github.com/0xdps/sqlite-hub/server/internal/files"
	"github.com/0xdps/sqlite-hub/server/internal/queue"
	"github.com/0xdps/sqlite-hub/server/internal/sysutil"
	"github.com/0xdps/sqlite-hub/server/internal/telemetry"
)

// MetricsHandler holds deps for the metrics route.
type MetricsHandler struct {
	cfg      *config.Config
	registry *db.Registry
	storage  *files.Storage
	queue    *queue.Queue
	tel      *telemetry.Counters
}

// NewMetricsHandler creates a MetricsHandler.
func NewMetricsHandler(cfg *config.Config, registry *db.Registry, storage *files.Storage, wq *queue.Queue, tel *telemetry.Counters) *MetricsHandler {
	return &MetricsHandler{cfg: cfg, registry: registry, storage: storage, queue: wq, tel: tel}
}

// Metrics handles GET /api/metrics (admin only).
func (h *MetricsHandler) Metrics(w http.ResponseWriter, r *http.Request) {
	dbs, err := h.registry.ListDatabases()
	if err != nil {
		ErrorJSON(w, http.StatusInternalServerError, err.Error())
		return
	}

	vol := sysutil.VolumeInfo(h.cfg.DataPath)

	type dbInfo struct {
		Name      string `json:"name"`
		SizeBytes int64  `json:"size_bytes"`
	}
	dbList := make([]dbInfo, 0, len(dbs))
	var largest *dbInfo
	for _, rec := range dbs {
		sz := sysutil.FileSizeBytes(filepath.Join(h.cfg.DataPath, rec.Name+".db"))
		dbList = append(dbList, dbInfo{Name: rec.Name, SizeBytes: sz})
		if largest == nil || sz > largest.SizeBytes {
			largest = &dbList[len(dbList)-1]
		}
	}

	fileStats, _ := h.storage.StorageMetrics()
	audit, _ := h.registry.GetAuditMetrics()
	snap := h.tel.Snapshot()
	queueDepth := h.queue.Depth()

	resp := map[string]any{
		"total_dbs":           len(dbs),
		"volume_used_bytes":   vol.Used,
		"volume_total_bytes":  vol.Total,
		"volume_used_percent": vol.UsedPercent,
		"databases":           dbList,
		"largest_db":          largest,
		"files": map[string]any{
			"total_files": fileStats.TotalFiles,
			"total_bytes": fileStats.TotalBytes,
			"by_database": fileStats.ByDatabase,
		},
		"audit": map[string]any{
			"total_events":     audit.TotalEvents,
			"events_last_24h":  audit.EventsLast24h,
			"by_type_last_24h": audit.ByTypeLast24h,
		},
		"exec": map[string]any{
			"reads":       snap.Reads,
			"writes":      snap.Writes,
			"errors":      snap.Errors,
			"queue_depth": queueDepth,
			"avg_exec_ms": snap.AvgExecMs,
		},
		"slo": map[string]any{
			"availability_pct": snap.AvailabilityPct,
			"error_rate_pct":   snap.ErrorRatePct,
			"avg_exec_ms":      snap.AvgExecMs,
		},
	}
	writeJSON(w, http.StatusOK, resp)
}
