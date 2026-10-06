package mockapi

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// OAuthIssuer is a minimal OAuth2 client-credentials token endpoint, standing
// in for an airline's real authorization server.
type OAuthIssuer struct {
	ClientID     string
	ClientSecret string
	TokenTTL     time.Duration

	mu     sync.Mutex
	tokens map[string]time.Time // access token -> expiry
}

func NewOAuthIssuer(clientID, clientSecret string, ttl time.Duration) *OAuthIssuer {
	return &OAuthIssuer{ClientID: clientID, ClientSecret: clientSecret, TokenTTL: ttl, tokens: make(map[string]time.Time)}
}

// TokenHandler serves POST {grant_type=client_credentials}, with the client
// authenticating via HTTP Basic auth or client_id/client_secret form fields.
func (o *OAuthIssuer) TokenHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeOAuthError(w, http.StatusMethodNotAllowed, "invalid_request")
		return
	}
	if err := r.ParseForm(); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	if r.PostForm.Get("grant_type") != "client_credentials" {
		writeOAuthError(w, http.StatusBadRequest, "unsupported_grant_type")
		return
	}

	id, secret, ok := r.BasicAuth()
	if ok {
		id, _ = url.QueryUnescape(id)
		secret, _ = url.QueryUnescape(secret)
	} else {
		id, secret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
	}
	if !secureEqual(id, o.ClientID) || !secureEqual(secret, o.ClientSecret) {
		writeOAuthError(w, http.StatusUnauthorized, "invalid_client")
		return
	}

	buf := make([]byte, 24)
	rand.Read(buf)
	token := hex.EncodeToString(buf)

	o.mu.Lock()
	now := time.Now()
	for t, exp := range o.tokens { // drop expired tokens
		if now.After(exp) {
			delete(o.tokens, t)
		}
	}
	o.tokens[token] = now.Add(o.TokenTTL)
	o.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"access_token": token,
		"token_type":   "Bearer",
		"expires_in":   int(o.TokenTTL.Seconds()),
	})
}

// Authorized reports whether the request carries a live bearer token.
func (o *OAuthIssuer) Authorized(r *http.Request) bool {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || token == "" {
		return false
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	exp, found := o.tokens[token]
	return found && time.Now().Before(exp)
}

func secureEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func writeOAuthError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	if status == http.StatusUnauthorized {
		w.Header().Set("WWW-Authenticate", `Basic realm="token"`)
	}
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": code})
}
