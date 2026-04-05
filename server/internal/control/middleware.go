// Package control — middleware.go provides HTTP middleware for control-mode
// routes. It validates the user session cookie against Redis and injects
// the resolved *User into the request context.
package control

import (
	"context"
	"net/http"

	"github.com/rs/zerolog/log"

	"github.com/0xdps/sqlite-hub/server/internal/cache"
)

// ctxKey is the context key for the authenticated control user.
type ctxKey struct{}

// UserFromCtx retrieves the authenticated user from the context.
// Returns nil when the request is unauthenticated.
func UserFromCtx(ctx context.Context) *User {
	u, _ := ctx.Value(ctxKey{}).(*User)
	return u
}

// RequireSession is HTTP middleware that validates the user session cookie.
// On success it injects the *User into the request context.
// On failure it returns 401 JSON.
func RequireSession(cdb *ControlDB, c cache.Client) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie("sqlitedbhub_session")
			if err != nil || cookie.Value == "" {
				writeError(w, http.StatusUnauthorized, "not authenticated")
				return
			}

			sv, err := c.GetSession(r.Context(), cookie.Value)
			if err != nil || sv == nil || sv.Role != "user" || sv.UserID == "" {
				writeError(w, http.StatusUnauthorized, "session invalid or expired")
				return
			}

			user, err := cdb.GetUser(sv.UserID)
			if err != nil || user == nil {
				log.Warn().Str("user_id", sv.UserID).Msg("[control] user not found in DB")
				writeError(w, http.StatusUnauthorized, "user not found")
				return
			}

			ctx := context.WithValue(r.Context(), ctxKey{}, user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
