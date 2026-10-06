package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flight-search/internal/models"
	"flight-search/internal/ratelimit"
	"flight-search/internal/retry"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

type resilientClient struct {
	httpClient *http.Client
	limiter    *ratelimit.Limiter
	retryCfg   retry.Config
	tokens     *tokenSource // nil for providers that need no auth
}

func newResilientClient(httpClient *http.Client, limiter *ratelimit.Limiter, retryCfg retry.Config) *resilientClient {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	if retryCfg.MaxAttempts == 0 {
		retryCfg = retry.DefaultConfig()
	}
	return &resilientClient{httpClient: httpClient, limiter: limiter, retryCfg: retryCfg}
}

// withClientCredentials makes every request carry an OAuth2 bearer token
// obtained via the client-credentials grant. Empty creds leave the client
// unauthenticated (e.g. when pointed at the open mock server).
func (c *resilientClient) withClientCredentials(creds ClientCredentials) *resilientClient {
	if creds.enabled() {
		c.tokens = newTokenSource(creds, c.httpClient)
	}
	return c
}

type configError struct{ err error }

func (e *configError) Error() string { return e.err.Error() }

func (e *configError) Unwrap() error { return e.err }

type httpStatusError struct {
	statusCode int
	body       string
}

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("provider returned HTTP %d: %s", e.statusCode, e.body)
}

func isRetryableHTTPError(err error) bool {
	if err == nil {
		return false
	}
	var cfgErr *configError
	if errors.As(err, &cfgErr) {
		return false
	}
	var statusErr *httpStatusError
	if errors.As(err, &statusErr) {
		return statusErr.statusCode >= http.StatusInternalServerError || statusErr.statusCode == http.StatusTooManyRequests
	}
	return true
}

// fetchJSON sends GET endpoint?origin=..&destination=..&date=.. -- the
// request shape of the mock airline APIs.
func (c *resilientClient) fetchJSON(ctx context.Context, endpoint string, params models.SearchParams, out interface{}) error {
	u, err := url.Parse(endpoint)
	if err != nil {
		return &configError{fmt.Errorf("invalid provider url %q: %w", endpoint, err)}
	}
	q := u.Query()
	q.Set("origin", params.Origin)
	q.Set("destination", params.Destination)
	q.Set("date", params.Date)
	u.RawQuery = q.Encode()

	return c.do(ctx, func(ctx context.Context) (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	}, out)
}

// postJSON sends POST endpoint with reqBody encoded as JSON, for providers
// whose search API takes a request body instead of query params.
func (c *resilientClient) postJSON(ctx context.Context, endpoint string, reqBody, out interface{}) error {
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return &configError{fmt.Errorf("encoding request body: %w", err)}
	}
	return c.do(ctx, func(ctx context.Context) (*http.Request, error) {
		// A fresh reader per attempt: a retried request must resend the body.
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		return req, nil
	}, out)
}

// do runs one logical request with rate limiting, retries and (if
// configured) bearer-token auth. newReq is called once per attempt.
func (c *resilientClient) do(ctx context.Context, newReq func(ctx context.Context) (*http.Request, error), out interface{}) error {
	return retry.Do(ctx, c.retryCfg, func(attempt int) error {
		if err := c.limiter.Wait(ctx); err != nil {
			return retry.Permanent(err)
		}
		var token string
		if c.tokens != nil {
			var err error
			if token, err = c.tokens.Token(ctx); err != nil {
				if !isRetryableHTTPError(err) {
					return retry.Permanent(err)
				}
				return err
			}
		}

		req, err := newReq(ctx)
		if err != nil {
			return retry.Permanent(&configError{fmt.Errorf("building request: %w", err)})
		}
		req.Header.Set("Accept", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}

		err = doJSON(c.httpClient, req, out)
		if token != "" && isUnauthorized(err) {
			// Token was revoked or expired early: drop it and retry with a
			// freshly issued one.
			c.tokens.Invalidate(token)
			return err
		}
		if err != nil && !isRetryableHTTPError(err) {
			return retry.Permanent(err)
		}
		return err
	})
}

func isUnauthorized(err error) bool {
	var statusErr *httpStatusError
	return errors.As(err, &statusErr) && statusErr.statusCode == http.StatusUnauthorized
}

func doJSON(client *http.Client, req *http.Request, out interface{}) error {
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return &httpStatusError{statusCode: resp.StatusCode, body: string(body)}
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("invalid JSON response: %w", err)
	}
	return nil
}
