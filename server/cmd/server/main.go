package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/0xdps/sqlite-hub/server/internal/auth"
	"github.com/0xdps/sqlite-hub/server/internal/cache"
	"github.com/0xdps/sqlite-hub/server/internal/config"
	"github.com/0xdps/sqlite-hub/server/internal/control"
	"github.com/0xdps/sqlite-hub/server/internal/db"
	"github.com/0xdps/sqlite-hub/server/internal/files"
	"github.com/0xdps/sqlite-hub/server/internal/handler"
	"github.com/0xdps/sqlite-hub/server/internal/middleware"
	"github.com/0xdps/sqlite-hub/server/internal/queue"
	"github.com/0xdps/sqlite-hub/server/internal/telemetry"
)

const version = "2.0.0-dev"

func main() {
	// ── Config ────────────────────────────────────────────────────────────────
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	// ── Logging ───────────────────────────────────────────────────────────────
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	level, err := zerolog.ParseLevel(cfg.LogLevel)
	if err != nil {
		level = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(level)

	if cfg.LogLevel == "debug" {
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	}

	log.Info().
		Str("mode", string(cfg.Mode)).
		Str("version", version).
		Int("port", cfg.Port).
		Msg("sqlite-hub server starting")

	// ── Cache ─────────────────────────────────────────────────────────────────
	cacheClient, err := cache.New(cfg.CacheMode, cfg.RedisURL)
	if err != nil {
		log.Fatal().Err(err).Msg("cache init failed")
	}
	{
		mode := cfg.CacheMode
		if mode == "" {
			if cfg.RedisURL != "" {
				mode = cache.ModeRedis
			} else {
				mode = cache.ModeNone
			}
		}
		log.Info().Str("mode", mode).Bool("available", cacheClient.Available()).Msg("cache ready")
	}

	// ── DB pool ───────────────────────────────────────────────────────────────
	pool := db.NewPool(cfg.DataPath)
	defer pool.Close()

	// ── Registry ──────────────────────────────────────────────────────────────
	registry, err := db.OpenRegistry(cfg.DataPath)
	if err != nil {
		log.Fatal().Err(err).Msg("registry open failed")
	}
	defer registry.Close()

	// ── File storage ──────────────────────────────────────────────────────────
	fileStorage, err := files.NewStorage(cfg.DataPath, cfg)
	if err != nil {
		log.Fatal().Err(err).Msg("file storage open failed")
	}
	defer fileStorage.Close()

	// ── Write queue ───────────────────────────────────────────────────────────
	wq := queue.New(cfg.MaxWriteQueueDepth)
	defer wq.Stop()
	// ── Telemetry counters ────────────────────────────────────────────────────────
	tel := telemetry.New()
	// ── Router ────────────────────────────────────────────────────────────────
	r := chi.NewRouter()

	// Global middleware
	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(middleware.StripInternalHeaders)
	r.Use(middleware.Logger)
	r.Use(chimw.Recoverer)
	r.Use(auth.CORS(cfg))
	r.Use(auth.ControlPlaneStamper(cfg))
	r.Use(auth.AdminStamper(cfg, cacheClient))

	// ── Handlers ──────────────────────────────────────────────────────────────
	dbH := handler.NewDBHandler(cfg, pool, registry)
	queryH := handler.NewQueryHandler(cfg, pool, registry, cacheClient, tel)
	execH := handler.NewExecHandler(cfg, pool, wq, registry, cacheClient, tel)
	filesH := handler.NewFilesHandler(cfg, registry, fileStorage, cacheClient)
	tokensH := handler.NewTokensHandler(cfg, registry, cacheClient)
	metricsH := handler.NewMetricsHandler(cfg, registry, fileStorage, wq, tel)
	maintenanceH := handler.NewMaintenanceHandler(cfg, registry, fileStorage)
	systemH := handler.NewSystemHandler(cfg)
	authH := auth.NewHandler(cfg, cacheClient)
	internalH := handler.NewInternalHandler(cacheClient)
	bucketFilesH := handler.NewBucketFilesHandler(cfg, fileStorage, cacheClient)

	// Health + version — no auth required
	r.Get("/api/health", handler.Health)
	r.Get("/api/version", handler.NewVersion(version, string(cfg.Mode), cacheClient.Available()))

	// Auth routes — public
	r.Post("/api/auth/login", authH.Login)
	r.Post("/api/auth/logout", authH.Logout)
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireAdmin)
		r.Get("/api/auth/me", authH.Me)
	})

	// ── Internal control-plane callbacks (control → template only) ───────────
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireControlPlane)
		r.Delete("/api/internal/cache/apikey/{hash}", internalH.InvalidateAPIKey)
	})

	// ── Admin-only DB management ───────────────────────────────────────────────
	// NOTE: /api/db/deleted must be registered BEFORE /api/db/{name} so chi does
	// not treat the literal "deleted" as a :name capture.
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireAdmin)
		r.Get("/api/db", dbH.ListDBs)
		r.Post("/api/db", dbH.CreateDB)
		r.Get("/api/db/deleted", dbH.ListDeletedDBs)
		r.Post("/api/db/deleted/{name}", dbH.DeletedDBAction)
		r.Get("/api/db/{name}", dbH.GetDB)
		r.Patch("/api/db/{name}", dbH.PatchDB)
		r.Delete("/api/db/{name}", dbH.DeleteDB)
		r.Get("/api/metrics", metricsH.Metrics)
		r.Post("/api/maintenance/cleanup", maintenanceH.Cleanup)
		// System / internal databases — read-only browse access.
		r.Get("/api/system/dbs", systemH.ListSystemDBs)
		r.Post("/api/system/db/{name}/query", systemH.QuerySystemDB)
	})

	// ── Per-DB data endpoints (auth handled inside each handler) ──────────────
	r.Post("/api/db/{name}/query", queryH.Query)
	r.Post("/api/db/{name}/exec", execH.Exec)
	r.Get("/api/db/{name}/files", filesH.List)
	r.Post("/api/db/{name}/files", filesH.Upload)
	// NOTE: presign/batch and bulk-delete must be registered before {id} routes
	// so chi does not treat "presign" or "bulk-delete" as a file ID.
	r.Post("/api/db/{name}/files/presign/batch", filesH.PresignBatch)
	r.Post("/api/db/{name}/files/bulk-delete", filesH.BulkDeleteFiles)
	r.Head("/api/db/{name}/files/{id}", filesH.HeadFile)
	r.Get("/api/db/{name}/files/{id}", filesH.Download)
	r.Delete("/api/db/{name}/files/{id}", filesH.DeleteFile)
	r.Get("/api/db/{name}/files/{id}/meta", filesH.Meta)
	r.Post("/api/db/{name}/files/{id}/presign", filesH.PresignFile)
	r.Post("/api/db/{name}/tokens/files", tokensH.CreateToken)
	r.Post("/api/db/{name}/tokens/files/revoke", tokensH.RevokeToken)

	// Public file shortlink — no admin session required, only a valid file token.
	// Registered outside the admin group; auth is enforced inside the handler.
	r.Get("/{dbName}/file/{id}", filesH.FileShortlink)

	// Legacy /api/v1/* prefix strip — forward to the same router without the prefix.
	// This provides backwards compatibility for older clients that used /api/v1/.
	r.Mount("/api/v1", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		p := req.URL.Path
		if len(p) >= 7 {
			p = "/api" + p[7:] // strip "/api/v1"
		}
		req.URL.Path = p
		if req.URL.RawPath != "" {
			req.URL.RawPath = "/api" + req.URL.RawPath[7:]
		}
		r.ServeHTTP(w, req)
	}))

	// ── Control-mode routes (UUID-based API keys) ────────────────────────────
	// All user management, plan enforcement, and provisioning is handled by
	// the control plane (Next.js). The Go server only provides data access.
	if cfg.Mode == config.ModeControl {
		r.Post("/api/query/{uuid}", queryH.QueryByUUID)
		r.Post("/api/exec/{uuid}", execH.ExecByUUID)
		r.Get("/api/files/{uuid}", filesH.ListByUUID)
		r.Post("/api/files/{uuid}", filesH.UploadByUUID)
		r.Head("/api/files/{uuid}/{id}", filesH.HeadFileByUUID)
		r.Get("/api/files/{uuid}/{id}", filesH.DownloadByUUID)
		r.Delete("/api/files/{uuid}/{id}", filesH.DeleteFileByUUID)

		// Bucket file routes — auth via shs_ API key scope (bucket:* or bucket:<name>).
		// NOTE: presign/batch and bulk-delete must be registered before {id} routes.
		r.Get("/api/buckets/{name}/files", bucketFilesH.List)
		r.Post("/api/buckets/{name}/files", bucketFilesH.Upload)
		r.Post("/api/buckets/{name}/files/presign/batch", bucketFilesH.PresignBatch)
		r.Post("/api/buckets/{name}/files/bulk-delete", bucketFilesH.BulkDeleteFiles)
		r.Head("/api/buckets/{name}/files/{id}", bucketFilesH.HeadFile)
		r.Get("/api/buckets/{name}/files/{id}", bucketFilesH.Download)
		r.Delete("/api/buckets/{name}/files/{id}", bucketFilesH.DeleteFile)
		r.Get("/api/buckets/{name}/files/{id}/meta", bucketFilesH.Meta)
		r.Post("/api/buckets/{name}/files/{id}/presign", bucketFilesH.PresignFile)

		// Initialise control.db schema so that ValidateAPIKey can read it.
		// The control plane writes user/key data here via the admin exec endpoint.
		cdb, err := control.Open(cfg.DataPath)
		if err != nil {
			log.Fatal().Err(err).Msg("control DB open failed")
		}
		defer cdb.Close()

		// Register "control" in registry.db so the standard query/exec handlers
		// can serve POST /api/db/control/query and /api/db/control/exec.
		// These endpoints are used by the control plane (Next.js) to read/write
		// user metadata. INSERT OR IGNORE makes this idempotent on restart.
		if existing, _ := registry.GetDatabase("control"); existing == nil {
			if _, err := registry.InsertDatabase("control", "_system", nil, nil); err != nil {
				log.Fatal().Err(err).Msg("control DB registry insert failed")
			}
			log.Info().Msg("registered 'control' database in registry")
		}
	}

	// ── Server ────────────────────────────────────────────────────────────────
	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      r,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		log.Info().Int("port", cfg.Port).Msg("listening")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("server error")
		}
	}()

	<-quit
	log.Info().Msg("shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Error().Err(err).Msg("shutdown error")
	}
	log.Info().Msg("stopped")
}
