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
	"github.com/0xdps/sqlite-hub/server/internal/db"
	"github.com/0xdps/sqlite-hub/server/internal/files"
	"github.com/0xdps/sqlite-hub/server/internal/handler"
	"github.com/0xdps/sqlite-hub/server/internal/middleware"
	"github.com/0xdps/sqlite-hub/server/internal/queue"
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

	// ── Cache (Redis optional) ────────────────────────────────────────────────
	var cacheClient cache.Client
	if cfg.RedisURL != "" {
		rc, err := cache.NewRedis(cfg.RedisURL)
		if err != nil {
			log.Fatal().Err(err).Msg("redis connect failed")
		}
		cacheClient = rc
		log.Info().Msg("redis connected")
	} else {
		cacheClient = cache.NewNoop()
		log.Info().Msg("redis not configured — using JWT sessions (standalone mode)")
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

	// ── Router ────────────────────────────────────────────────────────────────
	r := chi.NewRouter()

	// Global middleware
	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(middleware.StripInternalHeaders)
	r.Use(middleware.Logger)
	r.Use(chimw.Recoverer)
	r.Use(auth.CORS(cfg))
	r.Use(auth.AdminStamper(cfg, cacheClient))

	// ── Handlers ──────────────────────────────────────────────────────────────
	dbH := handler.NewDBHandler(cfg, pool, registry)
	queryH := handler.NewQueryHandler(cfg, pool, registry)
	execH := handler.NewExecHandler(cfg, pool, wq, registry)
	filesH := handler.NewFilesHandler(cfg, registry, fileStorage)
	tokensH := handler.NewTokensHandler(cfg, registry)
	metricsH := handler.NewMetricsHandler(cfg, registry, fileStorage)
	maintenanceH := handler.NewMaintenanceHandler(cfg, registry, fileStorage)
	authH := auth.NewHandler(cfg, cacheClient)

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
	})

	// ── Per-DB data endpoints (auth handled inside each handler) ──────────────
	r.Post("/api/db/{name}/query", queryH.Query)
	r.Post("/api/db/{name}/exec", execH.Exec)
	r.Get("/api/db/{name}/files", filesH.List)
	r.Post("/api/db/{name}/files", filesH.Upload)
	r.Head("/api/db/{name}/files/{id}", filesH.HeadFile)
	r.Get("/api/db/{name}/files/{id}", filesH.Download)
	r.Delete("/api/db/{name}/files/{id}", filesH.DeleteFile)
	r.Post("/api/db/{name}/tokens/files", tokensH.CreateToken)
	r.Post("/api/db/{name}/tokens/files/revoke", tokensH.RevokeToken)

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
