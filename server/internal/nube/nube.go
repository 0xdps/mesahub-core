// Package nube provides a minimal HTTP client for the NubeAuth OAuth/PKCE API.
//
// NubeAuth uses standard OAuth 2.0 with PKCE. This client implements:
//   - PKCE code verifier/challenge generation (S256)
//   - Authorization URL construction
//   - Authorization code exchange
//   - User profile fetch (/v1/me)
//   - Subscription details fetch (/v1/subscriptions/me)
package nube

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Client is a NubeAuth HTTP client scoped to a single app.
type Client struct {
	gatewayURL string
	appID      string
	appSecret  string
	httpClient *http.Client
}

// NewClient constructs a NubeAuth client using the provided credentials.
func NewClient(gatewayURL, appID, appSecret string) *Client {
	return &Client{
		gatewayURL: strings.TrimRight(gatewayURL, "/"),
		appID:      appID,
		appSecret:  appSecret,
		httpClient: &http.Client{},
	}
}

// PKCEParams holds the PKCE code verifier and challenge.
type PKCEParams struct {
	Verifier  string
	Challenge string // S256: base64url(sha256(verifier))
}

// GeneratePKCE creates a cryptographically random code verifier and its
// S256 code challenge.
func GeneratePKCE() (*PKCEParams, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return nil, fmt.Errorf("nube: generate pkce: %w", err)
	}
	verifier := base64.RawURLEncoding.EncodeToString(buf)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	return &PKCEParams{Verifier: verifier, Challenge: challenge}, nil
}

// BuildAuthURL returns the NubeAuth /v1/auth/start URL the browser should be
// redirected to. provider is the OAuth provider ("google" or "github").
// returnTo is the application callback URL where NubeAuth delivers the
// one-time exchange code as ?code=<code>. codeChallenge is the S256 PKCE
// challenge; NubeAuth round-trips it through the flow so it can be verified
// during token exchange.
func (c *Client) BuildAuthURL(provider, returnTo, codeChallenge string) string {
	u, _ := url.Parse(c.gatewayURL + "/v1/auth/start")
	q := u.Query()
	q.Set("provider", provider)
	q.Set("return_to", returnTo)
	q.Set("app_id", c.appID)
	q.Set("audience", "app")
	q.Set("code_challenge", codeChallenge)
	q.Set("code_challenge_method", "S256")
	u.RawQuery = q.Encode()
	return u.String()
}

// ExchangeCode exchanges the one-time code delivered by NubeAuth via the
// ?code= query parameter in the OAuth callback redirect for a session token.
// codeVerifier is the PKCE verifier generated at the start of the flow.
// Returns the session token on success; use it as a Bearer token for /v1/me
// and other authenticated endpoints.
func (c *Client) ExchangeCode(code, codeVerifier string) (string, error) {
	body := map[string]string{
		"code":          code,
		"app_id":        c.appID,
		"code_verifier": codeVerifier,
	}
	var resp struct {
		SessionToken string `json:"sessionToken"`
		Error        string `json:"error"`
	}
	if err := c.post("/v1/auth/token", body, &resp); err != nil {
		return "", err
	}
	if resp.Error != "" {
		return "", fmt.Errorf("nube: exchange code: %s", resp.Error)
	}
	return resp.SessionToken, nil
}

// User holds the NubeAuth user profile.
type User struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

// GetMe fetches the authenticated user's profile using the provided access token.
func (c *Client) GetMe(accessToken string) (*User, error) {
	var u User
	if err := c.getAuthed("/v1/me", accessToken, &u); err != nil {
		return nil, err
	}
	return &u, nil
}

// Subscription holds the plan and status returned by NubeAuth.
type Subscription struct {
	PlanSlug     string         `json:"planSlug"`
	Status       string         `json:"status"`
	Entitlements map[string]any `json:"entitlements"`
}

// GetSubscription fetches the authenticated user's subscription details.
// Returns a zero-value Subscription on error (caller should treat as free plan).
func (c *Client) GetSubscription(accessToken string) (*Subscription, error) {
	var s Subscription
	if err := c.getAuthed("/v1/subscriptions/me", accessToken, &s); err != nil {
		return &Subscription{PlanSlug: "free", Status: "active"}, nil
	}
	return &s, nil
}

// ── HTTP helpers ──────────────────────────────────────────────────────────────

func (c *Client) post(path string, body any, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, c.gatewayURL+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.appSecret != "" {
		req.Header.Set("X-Nube-App-Secret", c.appSecret)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("nube: POST %s: %w", path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("nube: POST %s: status %d — %s", path, resp.StatusCode, raw)
	}
	return json.Unmarshal(raw, out)
}

func (c *Client) getAuthed(path, token string, out any) error {
	req, err := http.NewRequest(http.MethodGet, c.gatewayURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if c.appSecret != "" {
		req.Header.Set("X-Nube-App-Secret", c.appSecret)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("nube: GET %s: %w", path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("nube: GET %s: status %d — %s", path, resp.StatusCode, raw)
	}
	return json.Unmarshal(raw, out)
}
