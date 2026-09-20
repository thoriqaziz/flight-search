package providers

import (
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

func (c *resilientClient) fetchJSON(ctx context.Context, endpoint string, params models.SearchParams, out interface{}) error {
	return retry.Do(ctx, c.retryCfg, func(attempt int) error {
		if err := c.limiter.Wait(ctx); err != nil {
			return retry.Permanent(err)
		}
		err := doFetchJSON(ctx, c.httpClient, endpoint, params, out)
		if err != nil && !isRetryableHTTPError(err) {
			return retry.Permanent(err)
		}
		return err
	})
}

func doFetchJSON(ctx context.Context, client *http.Client, endpoint string, params models.SearchParams, out interface{}) error {
	u, err := url.Parse(endpoint)
	if err != nil {
		return &configError{fmt.Errorf("invalid provider url %q: %w", endpoint, err)}
	}
	q := u.Query()
	q.Set("origin", params.Origin)
	q.Set("destination", params.Destination)
	q.Set("date", params.Date)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return &configError{fmt.Errorf("building request: %w", err)}
	}

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
