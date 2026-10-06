package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// ClientCredentials configures the OAuth2 client-credentials grant
// (RFC 6749 §4.4): the service trades a client ID/secret for a short-lived
// bearer token, then sends that token on every API call.
type ClientCredentials struct {
	TokenURL     string
	ClientID     string
	ClientSecret string
	Scope        string // optional, space-separated
}

func (c ClientCredentials) enabled() bool { return c.ClientID != "" }

// tokenExpirySkew refreshes a token slightly before it actually expires so a
// request never goes out with a token that dies in flight.
const tokenExpirySkew = 30 * time.Second

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"` // seconds
}

// tokenSource fetches and caches an access token, shared by all concurrent
// searches against one provider.
type tokenSource struct {
	creds      ClientCredentials
	httpClient *http.Client

	mu        sync.Mutex
	token     string
	expiresAt time.Time
}

func newTokenSource(creds ClientCredentials, httpClient *http.Client) *tokenSource {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &tokenSource{creds: creds, httpClient: httpClient}
}

// Token returns a cached token, fetching a new one if missing or expiring.
// The lock is held across the fetch so concurrent callers wait for a single
// token request instead of all hitting the token endpoint at once.
func (s *tokenSource) Token(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.token != "" && time.Now().Before(s.expiresAt) {
		return s.token, nil
	}
	tok, err := s.fetch(ctx)
	if err != nil {
		return "", err
	}
	s.token = tok.AccessToken
	ttl := time.Duration(tok.ExpiresIn)*time.Second - tokenExpirySkew
	if ttl <= 0 {
		ttl = time.Duration(tok.ExpiresIn) * time.Second / 2
	}
	s.expiresAt = time.Now().Add(ttl)
	return s.token, nil
}

// Invalidate drops the cached token (e.g. after the API answers 401 because
// the token was revoked early) so the next Token call fetches a fresh one.
func (s *tokenSource) Invalidate(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.token == token {
		s.token = ""
	}
}

func (s *tokenSource) fetch(ctx context.Context) (tokenResponse, error) {
	form := url.Values{"grant_type": {"client_credentials"}}
	if s.creds.Scope != "" {
		form.Set("scope", s.creds.Scope)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.creds.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return tokenResponse{}, &configError{fmt.Errorf("building token request: %w", err)}
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.SetBasicAuth(url.QueryEscape(s.creds.ClientID), url.QueryEscape(s.creds.ClientSecret))

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return tokenResponse{}, fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return tokenResponse{}, fmt.Errorf("reading token response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		// 400/401 here means bad client ID/secret -- retrying won't help.
		if resp.StatusCode < http.StatusInternalServerError && resp.StatusCode != http.StatusTooManyRequests {
			return tokenResponse{}, &configError{fmt.Errorf("token endpoint returned HTTP %d: %s", resp.StatusCode, body)}
		}
		return tokenResponse{}, &httpStatusError{statusCode: resp.StatusCode, body: string(body)}
	}

	var tok tokenResponse
	if err := json.Unmarshal(body, &tok); err != nil {
		return tokenResponse{}, fmt.Errorf("invalid token response: %w", err)
	}
	if tok.AccessToken == "" {
		return tokenResponse{}, fmt.Errorf("token response has no access_token")
	}
	if tok.TokenType != "" && !strings.EqualFold(tok.TokenType, "bearer") {
		return tokenResponse{}, &configError{fmt.Errorf("unsupported token_type %q", tok.TokenType)}
	}
	return tok, nil
}
