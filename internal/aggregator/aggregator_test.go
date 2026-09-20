package aggregator

import (
	"context"
	"strings"
	"testing"
	"time"

	"flight-search/internal/cache"
	"flight-search/internal/models"
	"flight-search/internal/providers"
)

// delayStubProvider simulates a provider whose HTTP call takes delay to
// respond. Like a real HTTP client honoring a request context, it returns
// early with ctx.Err() if ctx is cancelled/times out before delay elapses --
// this is what makes the timeout tests below meaningful rather than trivial.
type delayStubProvider struct {
	name    string
	delay   time.Duration
	flights []models.Flight
}

func (s delayStubProvider) Name() string { return s.name }

func (s delayStubProvider) Search(ctx context.Context, params models.SearchParams) ([]models.Flight, error) {
	select {
	case <-time.After(s.delay):
		return s.flights, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// flightFrom builds a flight distinct to provider -- a distinct flight
// number, since dedupeKey groups by (flight number, route, departure time)
// and these tests need to tell "3 different flights from 3 providers" apart
// from "the same flight quoted by 3 providers" (which mergeDuplicates would,
// correctly, collapse into one).
func flightFrom(provider string) models.Flight {
	flightNumber := "FL_" + provider
	return models.Flight{
		ID:            provider + "_" + flightNumber,
		Provider:      provider,
		FlightNumber:  flightNumber,
		Departure:     models.Endpoint{Airport: "CGK", DateTime: "2025-12-15T08:00:00+07:00"},
		Arrival:       models.Endpoint{Airport: "DPS", DateTime: "2025-12-15T10:00:00+07:00"},
		Price:         models.Price{Amount: 500000, Currency: "IDR"},
		DepartureTime: mustParseRFC3339("2025-12-15T08:00:00+07:00"),
		ArrivalTime:   mustParseRFC3339("2025-12-15T10:00:00+07:00"),
	}
}

func mustParseRFC3339(v string) time.Time {
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		panic(err)
	}
	return t
}

func searchParams() models.SearchParams {
	return models.SearchParams{Origin: "CGK", Destination: "DPS", Date: "2025-12-15"}
}

// TestSearch_QueriesProvidersInParallel proves providers are actually
// queried concurrently rather than one after another: three providers that
// each take ~60ms should together take close to 60ms, not ~180ms.
func TestSearch_QueriesProvidersInParallel(t *testing.T) {
	const perProviderDelay = 60 * time.Millisecond
	provs := []providers.Provider{
		delayStubProvider{name: "A", delay: perProviderDelay, flights: []models.Flight{flightFrom("A")}},
		delayStubProvider{name: "B", delay: perProviderDelay, flights: []models.Flight{flightFrom("B")}},
		delayStubProvider{name: "C", delay: perProviderDelay, flights: []models.Flight{flightFrom("C")}},
	}
	agg := New(provs, cache.New(time.Minute), time.Second)

	start := time.Now()
	flights, meta, err := agg.Search(context.Background(), searchParams(), false)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if meta.ProvidersSucceeded != 3 {
		t.Fatalf("expected all 3 providers to succeed, got %d", meta.ProvidersSucceeded)
	}
	if len(flights) != 3 {
		t.Fatalf("expected 3 merged flights, got %d", len(flights))
	}

	// Sequential execution would take ~3*perProviderDelay (~180ms); parallel
	// execution should take close to one delay. Leave generous headroom for
	// scheduler jitter in CI while still clearly distinguishing the two.
	if elapsed >= 2*perProviderDelay {
		t.Fatalf("expected parallel execution (~%v), took %v -- looks sequential", perProviderDelay, elapsed)
	}
}

// TestSearch_SlowProviderTimesOutWithoutBlockingOthers proves a provider
// exceeding its own timeout is cut off and reported as a failure, without
// delaying the overall search anywhere near that provider's actual latency,
// and without preventing the other providers' results from coming back.
func TestSearch_SlowProviderTimesOutWithoutBlockingOthers(t *testing.T) {
	const providerTimeout = 50 * time.Millisecond
	provs := []providers.Provider{
		delayStubProvider{name: "slow", delay: time.Second, flights: []models.Flight{flightFrom("slow")}},
		delayStubProvider{name: "fast1", delay: 5 * time.Millisecond, flights: []models.Flight{flightFrom("fast1")}},
		delayStubProvider{name: "fast2", delay: 5 * time.Millisecond, flights: []models.Flight{flightFrom("fast2")}},
	}
	agg := New(provs, cache.New(time.Minute), providerTimeout)

	start := time.Now()
	flights, meta, err := agg.Search(context.Background(), searchParams(), false)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The slow provider's own 1s delay must not leak into the overall
	// search time -- it should be cut off at ~providerTimeout.
	if elapsed >= 500*time.Millisecond {
		t.Fatalf("expected the slow provider's timeout to bound total search time near %v, took %v", providerTimeout, elapsed)
	}

	if meta.ProvidersQueried != 3 {
		t.Errorf("expected 3 providers queried, got %d", meta.ProvidersQueried)
	}
	if meta.ProvidersSucceeded != 2 {
		t.Errorf("expected 2 providers to succeed, got %d", meta.ProvidersSucceeded)
	}
	if meta.ProvidersFailed != 1 {
		t.Errorf("expected 1 provider to fail (timeout), got %d", meta.ProvidersFailed)
	}
	if len(meta.ProviderErrors) != 1 || meta.ProviderErrors[0].Provider != "slow" {
		t.Fatalf("expected a provider_errors entry naming \"slow\", got %+v", meta.ProviderErrors)
	}
	if !strings.Contains(meta.ProviderErrors[0].Error, "deadline exceeded") {
		t.Errorf("expected the recorded error to mention a deadline, got %q", meta.ProviderErrors[0].Error)
	}

	if len(flights) != 2 {
		t.Fatalf("expected the 2 fast providers' flights to still come back, got %d", len(flights))
	}
}

// TestSearch_AllProvidersFailingReturnsEmptyResultNotError proves a total
// provider outage degrades to an empty, well-formed result rather than
// Search itself returning an error.
func TestSearch_AllProvidersFailingReturnsEmptyResultNotError(t *testing.T) {
	const providerTimeout = 20 * time.Millisecond
	provs := []providers.Provider{
		delayStubProvider{name: "A", delay: time.Second},
		delayStubProvider{name: "B", delay: time.Second},
	}
	agg := New(provs, cache.New(time.Minute), providerTimeout)

	flights, meta, err := agg.Search(context.Background(), searchParams(), false)
	if err != nil {
		t.Fatalf("expected no error even when every provider fails, got %v", err)
	}
	if len(flights) != 0 {
		t.Fatalf("expected 0 flights, got %d", len(flights))
	}
	if meta.ProvidersSucceeded != 0 || meta.ProvidersFailed != 2 {
		t.Errorf("expected 0 succeeded / 2 failed, got %d succeeded / %d failed", meta.ProvidersSucceeded, meta.ProvidersFailed)
	}
}

// TestSearch_RequestContextCancellationStopsAllProviders proves that
// cancelling the caller's context (e.g. an HTTP client disconnecting) is
// honored even by a provider whose own per-provider timeout hasn't expired
// yet, since each provider's context is derived from the caller's.
func TestSearch_RequestContextCancellationStopsAllProviders(t *testing.T) {
	provs := []providers.Provider{
		delayStubProvider{name: "A", delay: time.Second},
	}
	// A generous per-provider timeout, but the caller's own context will be
	// cancelled almost immediately.
	agg := New(provs, cache.New(time.Minute), 10*time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, meta, err := agg.Search(ctx, searchParams(), false)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if elapsed >= 500*time.Millisecond {
		t.Fatalf("expected caller context cancellation to cut the provider off quickly, took %v", elapsed)
	}
	if meta.ProvidersFailed != 1 {
		t.Errorf("expected the provider to be recorded as failed once the caller's context was cancelled, got %d failed", meta.ProvidersFailed)
	}
}
