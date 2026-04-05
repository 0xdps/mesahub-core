// Package entitlements derives per-plan resource limits from NubeAuth license
// data, mirroring the TypeScript entitlements.ts in the control plane frontend.
//
// Primary source: license.entitlements from NubeAuth (configured per plan in
// the NubeAuth dashboard). Falls back to the conservative plan defaults below
// so the service still works when NubeAuth is unreachable.
package entitlements

// Plan holds the resolved resource limits for a user.
type Plan struct {
	MaxDatabases   int // -1 = unlimited
	MaxDbSizeMB    int // -1 = unlimited
	MonthlyQueries int // -1 = unlimited
}

// planDefaults maps NubeAuth plan slugs to fallback limits.
// Slugs must match what is configured in the NubeAuth dashboard.
var planDefaults = map[string]Plan{
	"free":      {MaxDatabases: 1, MaxDbSizeMB: 100, MonthlyQueries: 100_000},
	"builder":   {MaxDatabases: 5, MaxDbSizeMB: 1024, MonthlyQueries: -1},
	"pro":       {MaxDatabases: 15, MaxDbSizeMB: 5120, MonthlyQueries: -1},
	"dedicated": {MaxDatabases: -1, MaxDbSizeMB: -1, MonthlyQueries: -1},
}

var freeDefaults = Plan{MaxDatabases: 1, MaxDbSizeMB: 100, MonthlyQueries: 100_000}

// Resolve derives a Plan from the NubeAuth license entitlements map, falling
// back to planDefaults[planSlug] for any missing key.
//
// raw is the entitlements map from NubeAuth (may be nil).
func Resolve(planSlug string, raw map[string]any) Plan {
	if planSlug == "" {
		planSlug = "free"
	}
	fallback, ok := planDefaults[planSlug]
	if !ok {
		fallback = freeDefaults
	}
	if raw == nil {
		return fallback
	}

	return Plan{
		MaxDatabases:   numOr(raw, "max_databases", fallback.MaxDatabases),
		MaxDbSizeMB:    numOr(raw, "max_db_size_mb", fallback.MaxDbSizeMB),
		MonthlyQueries: numOr(raw, "monthly_queries", fallback.MonthlyQueries),
	}
}

func numOr(m map[string]any, key string, def int) int {
	v, ok := m[key]
	if !ok {
		return def
	}
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return def
}
