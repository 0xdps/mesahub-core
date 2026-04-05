// Package control — usage.go handles the GET /api/user/usage endpoint.
// Returns current-month usage counters, plan entitlements, and active database
// count for the authenticated control-plane user.
package control

import (
	"net/http"

	"github.com/rs/zerolog/log"

	"github.com/0xdps/sqlite-hub/server/internal/entitlements"
	"github.com/0xdps/sqlite-hub/server/internal/sysutil"
)

// GetUsage handles GET /api/user/usage.
// Response shape mirrors the UsageData TypeScript interface in control/src/app/dashboard/client.tsx.
func (h *Handler) GetUsage(w http.ResponseWriter, r *http.Request) {
	user := UserFromCtx(r.Context())

	usage, err := h.cdb.GetCurrentUsage(user.ID)
	if err != nil {
		log.Error().Err(err).Msg("[control/usage] GetCurrentUsage")
		writeError(w, http.StatusInternalServerError, "failed to load usage")
		return
	}

	activeCount, err := h.cdb.CountActiveDatabases(user.ID)
	if err != nil {
		log.Error().Err(err).Msg("[control/usage] CountActiveDatabases")
		activeCount = 0
	}

	// Refresh storage_bytes from filesystem so the value stays accurate.
	dbs, _ := h.cdb.ListDatabases(user.ID)
	var totalStorage int64
	for _, db := range dbs {
		totalStorage += sysutil.FileSizeBytes(sysutil.DBPath(h.cfg.DataPath, db.Name))
	}

	plan := entitlements.Resolve(user.NubePlan, nil)

	writeJSON(w, http.StatusOK, map[string]any{
		"period": map[string]any{
			"year":  usage.PeriodYear,
			"month": usage.PeriodMonth,
		},
		"usage": map[string]any{
			"queries_executed": usage.QueriesExecuted,
			"api_calls":        usage.APICalls,
			"storage_bytes":    totalStorage,
			"calls_success":    usage.CallsSuccess,
			"calls_client_err": usage.CallsClientErr,
			"calls_server_err": usage.CallsServerErr,
		},
		"entitlements": map[string]any{
			"maxDatabases":   plan.MaxDatabases,
			"maxDbSizeMb":    plan.MaxDbSizeMB,
			"monthlyQueries": plan.MonthlyQueries,
		},
		"databases": map[string]any{
			"active": activeCount,
		},
	})
}
