package config

import (
	"fmt"
	"os"
	"strconv"
)

// Mode controls which route groups are registered at startup.
type Mode string

const (
	ModeStandalone Mode = "standalone"
	ModeControl    Mode = "control"
)

type Config struct {
	// Core
	Port     int
	DataPath string
	Mode     Mode

	// Auth
	AdminToken         string
	SessionSecret      string
	ControlPlaneSecret string // secret for control→template internal callbacks; required in control mode

	// Redis — cache is Redis-backed when REDIS_URL is set, no-op otherwise.
	RedisURL string

	// Control-mode extras (only validated when Mode == ModeControl)
	CORSOrigins string

	// Operational limits
	MaxVolumeUsagePct       int
	MaxSQLLength            int
	MaxSQLBindings          int
	MaxWriteQueueDepth      int
	FileMaxSizeBytes        int64
	FileBulkDeleteMaxIDs    int
	EnableFileProxyDelivery bool
	LogLevel                string
}

func Load() (*Config, error) {
	cfg := &Config{
		Port:                    intEnv("PORT", 3000),
		DataPath:                strEnv("DATA_PATH", "/data"),
		Mode:                    Mode(strEnv("SQLITE_HUB_MODE", "standalone")),
		AdminToken:              os.Getenv("ADMIN_TOKEN"),
		SessionSecret:           os.Getenv("SESSION_SECRET"),
		ControlPlaneSecret:      os.Getenv("CONTROL_PLANE_SECRET"),
		RedisURL:                os.Getenv("REDIS_URL"),
		CORSOrigins:             strEnv("CORS_ALLOWED_ORIGINS", ""),
		MaxVolumeUsagePct:       intEnv("MAX_VOLUME_USAGE_PERCENT", 85),
		MaxSQLLength:            intEnv("MAX_SQL_LENGTH", 100_000),
		MaxSQLBindings:          intEnv("MAX_SQL_BINDINGS", 5000),
		MaxWriteQueueDepth:      intEnv("MAX_WRITE_QUEUE_DEPTH", 256),
		FileMaxSizeBytes:        int64(intEnv("FILE_MAX_SIZE_BYTES", 104_857_600)), // 100 MB
		FileBulkDeleteMaxIDs:    intEnv("FILE_BULK_DELETE_MAX_IDS", 100),
		EnableFileProxyDelivery: strEnv("ENABLE_FILE_PROXY_DELIVERY", "true") != "false",
		LogLevel:                strEnv("LOG_LEVEL", "info"),
	}

	if cfg.AdminToken == "" {
		return nil, fmt.Errorf("ADMIN_TOKEN is required")
	}
	if cfg.SessionSecret == "" {
		return nil, fmt.Errorf("SESSION_SECRET is required")
	}
	if cfg.Mode != ModeStandalone && cfg.Mode != ModeControl {
		return nil, fmt.Errorf("SQLITE_HUB_MODE must be 'standalone' or 'control', got %q", cfg.Mode)
	}
	if cfg.Mode == ModeControl && cfg.ControlPlaneSecret == "" {
		return nil, fmt.Errorf("CONTROL_PLANE_SECRET is required when SQLITE_HUB_MODE=control")
	}
	if os.Getenv("FILE_TOKEN_SIGNING_SECRET") == "" {
		return nil, fmt.Errorf("FILE_TOKEN_SIGNING_SECRET is required")
	}

	return cfg, nil
}

func strEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func intEnv(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %s has invalid integer value %q — using default %d\n", key, v, fallback)
		return fallback
	}
	if n < 0 {
		fmt.Fprintf(os.Stderr, "config: %s has negative value %d — using default %d\n", key, n, fallback)
		return fallback
	}
	return n
}
