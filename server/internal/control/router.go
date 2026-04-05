// Package control — router.go registers all control-plane HTTP routes.
package control

import (
	"github.com/go-chi/chi/v5"

	"github.com/0xdps/sqlite-hub/server/internal/cache"
)

// RegisterRoutes attaches the control plane endpoints to the given chi.Router.
// Only called from main.go when cfg.Mode == config.ModeControl.
func RegisterRoutes(r chi.Router, h *Handler, cdb *ControlDB, c cache.Client) {
	// Public auth endpoints — no session required.
	r.Get("/api/control/auth/login", h.Login)
	r.Get("/api/control/auth/callback", h.Callback)
	r.Post("/api/control/auth/logout", h.Logout)

	// Protected endpoints — RequireSession middleware applied to the group.
	r.Group(func(r chi.Router) {
		r.Use(RequireSession(cdb, h.cache))

		r.Get("/api/control/auth/me", h.Me)

		r.Get("/api/control/databases", h.ListDatabases)
		r.Post("/api/control/databases", h.CreateDatabase)
		r.Delete("/api/control/databases/{id}", h.DeleteDatabase)

		r.Get("/api/control/apikeys", h.ListAPIKeys)
		r.Post("/api/control/apikeys", h.CreateAPIKey)
		r.Delete("/api/control/apikeys/{id}", h.RevokeAPIKey)

		r.Get("/api/user/usage", h.GetUsage)
	})
}
