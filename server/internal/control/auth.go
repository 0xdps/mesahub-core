// Package control — auth.go implements the NubeAuth OAuth PKCE handlers for
// MODE=control:
//
//	GET  /api/control/auth/login     — initiate OAuth; redirect to NubeAuth
//	GET  /api/control/auth/callback  — exchange code, UPSERT user, issue session
//	POST /api/control/auth/logout    — destroy session
//	GET  /api/control/auth/me        — return current user
package control

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/0xdps/sqlite-hub/server/internal/cache"
	"github.com/0xdps/sqlite-hub/server/internal/config"
	"github.com/0xdps/sqlite-hub/server/internal/db"
	"github.com/0xdps/sqlite-hub/server/internal/nube"
)

// ── Handler ───────────────────────────────────────────────────────────────────

// Handler bundles dependencies for the control-plane HTTP handlers.
type Handler struct {
	cfg      *config.Config
	cdb      *ControlDB
	cache    cache.Client
	nube     *nube.Client
	registry *db.Registry
	pool     *db.Pool
}

// NewHandler constructs a control Handler.
func NewHandler(cfg *config.Config, cdb *ControlDB, c cache.Client, registry *db.Registry, pool *db.Pool) *Handler {
	var nc *nube.Client
	if cfg.NubeGatewayURL != "" {
		nc = nube.NewClient(cfg.NubeGatewayURL, cfg.NubeAppID, cfg.NubeAppSecret)
	}
	return &Handler{cfg: cfg, cdb: cdb, cache: c, nube: nc, registry: registry, pool: pool}
}

// ── Auth — OAuth PKCE ─────────────────────────────────────────────────────────

const pkceRedisTTL = 10 * time.Minute
const sessionTTL = 30 * 24 * time.Hour

// Login initiates the OAuth PKCE flow.
// GET /api/control/auth/login?redirect_uri=<callback_url>&return_to=<path>
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	redirectURI := r.URL.Query().Get("redirect_uri")
	if redirectURI == "" {
		writeError(w, http.StatusBadRequest, "redirect_uri is required")
		return
	}
	if h.nube == nil {
		writeError(w, http.StatusServiceUnavailable, "NubeAuth is not configured")
		return
	}

	pkce, err := nube.GeneratePKCE()
	if err != nil {
		log.Error().Err(err).Msg("[control/auth] PKCE generation failed")
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	state := randomHexN(16)

	// Persist PKCE state in Redis (required in control mode).
	if err := h.cache.SetPKCE(r.Context(), state, cache.PKCEValue{
		Verifier:    pkce.Verifier,
		RedirectURI: redirectURI,
		ReturnTo:    r.URL.Query().Get("return_to"),
	}, pkceRedisTTL); err != nil {
		log.Error().Err(err).Msg("[control/auth] failed to store PKCE state")
		writeError(w, http.StatusInternalServerError, "session error")
		return
	}

	// NubeAuth does not echo the app's state in the callback redirect.
	// Bind the state to this browser session via a short-lived cookie so the
	// callback handler can look up the PKCE verifier without needing a state
	// query parameter.
	secure := strings.HasPrefix(h.cfg.PublicURL(), "https://")
	http.SetCookie(w, &http.Cookie{
		Name:     "_pkce",
		Value:    state,
		Path:     "/api/control/auth",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(pkceRedisTTL.Seconds()),
	})

	provider := r.URL.Query().Get("provider")
	if provider == "" {
		provider = "google"
	}
	authURL := h.nube.BuildAuthURL(provider, redirectURI, pkce.Challenge)
	http.Redirect(w, r, authURL, http.StatusFound)
}

// Callback handles the OAuth authorization code callback.
// GET /api/control/auth/callback?code=<code>
func (h *Handler) Callback(w http.ResponseWriter, r *http.Request) {
	appURL := h.cfg.PublicURL() // front-end root URL
	code := r.URL.Query().Get("code")

	if code == "" {
		http.Redirect(w, r, appURL+"/login?error=missing_params", http.StatusFound)
		return
	}

	// NubeAuth delivers ?code=<exchange_code> but does not echo the app's state.
	// Recover the state (Redis key) from the short-lived cookie set during Login.
	pkceStateCookie, cookieErr := r.Cookie("_pkce")
	if cookieErr != nil || pkceStateCookie.Value == "" {
		log.Warn().Msg("[control/auth] PKCE state cookie missing or empty")
		http.Redirect(w, r, appURL+"/login?error=session_expired", http.StatusFound)
		return
	}
	state := pkceStateCookie.Value

	// Clear the cookie immediately — it is single-use.
	secureCallback := strings.HasPrefix(appURL, "https://")
	http.SetCookie(w, &http.Cookie{
		Name:     "_pkce",
		Value:    "",
		Path:     "/api/control/auth",
		HttpOnly: true,
		Secure:   secureCallback,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})

	// ── Retrieve and validate PKCE state ─────────────────────────────────────
	pkceState, err := h.cache.GetPKCE(r.Context(), state)
	if err != nil || pkceState == nil {
		log.Warn().Str("state", state).Msg("[control/auth] PKCE state not found or expired")
		http.Redirect(w, r, appURL+"/login?error=session_expired", http.StatusFound)
		return
	}
	_ = h.cache.DeletePKCE(r.Context(), state) // consume — one-time use

	// ── Step 1: exchange code ─────────────────────────────────────────────────
	accessToken, err := h.nube.ExchangeCode(code, pkceState.Verifier)
	if err != nil {
		log.Error().Err(err).Msg("[control/auth] code exchange failed")
		http.Redirect(w, r, appURL+"/login?error=exchange_failed", http.StatusFound)
		return
	}

	// ── Step 2: fetch user profile ────────────────────────────────────────────
	nubeUser, err := h.nube.GetMe(accessToken)
	if err != nil {
		log.Error().Err(err).Msg("[control/auth] GetMe failed")
		http.Redirect(w, r, appURL+"/login?error=profile_fetch_failed", http.StatusFound)
		return
	}

	// ── Step 3: fetch subscription (non-fatal) ────────────────────────────────
	sub, _ := h.nube.GetSubscription(accessToken)
	planSlug, planStatus := "free", "active"
	if sub != nil {
		if sub.PlanSlug != "" {
			planSlug = sub.PlanSlug
		}
		if sub.Status != "" {
			planStatus = sub.Status
		}
	}

	// ── Step 4: UPSERT user in control.db ────────────────────────────────────
	user, err := h.cdb.UpsertUser(nubeUser.ID, nubeUser.Email, planSlug, planStatus)
	if err != nil {
		log.Error().Err(err).Msg("[control/auth] user upsert failed")
		http.Redirect(w, r, appURL+"/login?error=db_error", http.StatusFound)
		return
	}

	// ── Step 5: issue session ─────────────────────────────────────────────────
	sessionID := randomHexN(24)
	if err := h.cache.SetSession(r.Context(), sessionID, cache.SessionValue{
		Role:   "user",
		UserID: user.ID,
		Email:  user.Email,
	}, sessionTTL); err != nil {
		log.Error().Err(err).Msg("[control/auth] session store failed")
		http.Redirect(w, r, appURL+"/login?error=session_error", http.StatusFound)
		return
	}

	secure := strings.HasPrefix(appURL, "https://")
	sameSite := http.SameSiteNoneMode // cross-origin (Vercel ↔ Railway)
	if !secure {
		sameSite = http.SameSiteLaxMode // SameSite=None requires Secure; use Lax on HTTP
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "sqlitedbhub_session",
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: sameSite,
		MaxAge:   int(sessionTTL.Seconds()),
	})

	returnTo := pkceState.ReturnTo
	if returnTo == "" || !startsWith(returnTo, "/") {
		returnTo = "/dashboard"
	}
	http.Redirect(w, r, appURL+returnTo, http.StatusFound)
	log.Info().Str("user", user.Email).Str("plan", planSlug).Msg("[control/auth] login successful")
}

// Logout destroys the current user session.
// POST /api/control/auth/logout
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("sqlitedbhub_session")
	if err == nil && cookie.Value != "" {
		_ = h.cache.DeleteSession(r.Context(), cookie.Value)
	}
	appURL := h.cfg.PublicURL()
	secure := strings.HasPrefix(appURL, "https://")
	sameSite := http.SameSiteNoneMode
	if !secure {
		sameSite = http.SameSiteLaxMode
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "sqlitedbhub_session",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: sameSite,
	})
	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// Me returns the current user's profile and plan.
// GET /api/control/auth/me  — protected by RequireControlSession middleware
func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	user := UserFromCtx(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	writeJSON(w, http.StatusOK, user)
}

// ── JSON helpers ──────────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func randomHexN(n int) string {
	b := make([]byte, n)
	rand.Read(b) //nolint:errcheck
	return hex.EncodeToString(b)
}

func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
