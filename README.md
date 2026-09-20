# Flight Search & Aggregation Service

A Go backend that aggregates flight search results from four mock airline
APIs (AirAsia, Batik Air, Garuda Indonesia, Lion Air), normalizes their
wildly different data formats into one schema, and serves filtered, sorted,
ranked results over HTTP.

## Why two binaries

`cmd/mockserver` serves the static fixtures in `input/` over real HTTP
endpoints, standing in for the four airlines' actual APIs. `cmd/server` is
the aggregation service itself and talks to those endpoints exactly as it
would talk to production airline APIs — real network calls, real timeouts,
real partial failures — rather than special-casing local file reads. This
also means `cmd/server`'s provider URLs can be pointed at real airline APIs
later by just changing environment variables.

## Running it

```
go run ./cmd/mockserver      # terminal 1, listens on :8081
go run ./cmd/server          # terminal 2, listens on :8080
```

```
curl "http://localhost:8080/api/v1/search?origin=CGK&destination=DPS&date=2025-12-15"
```

Run the test suite with `go test ./...`.

## API

`GET /api/v1/search`

Required:
- `origin`, `destination` — IATA airport codes
- `date` — `YYYY-MM-DD`

Optional search params:
- `passengers` (default 1), `cabin_class` (default `economy`)
- `refresh` or `no_cache` (`true`/`false`, default `false`) — skip reading the cached result for this exact search and re-query providers, refreshing the cache. Useful right after editing a fixture in `input/` when you don't want to wait out `CACHE_TTL_SECONDS`.

Filters (all optional, combine with AND):
- `min_price`, `max_price`
- `max_stops`
- `airlines` — comma-separated IATA codes or names, e.g. `airlines=QZ,Garuda Indonesia`
- `depart_after`, `depart_before`, `arrive_after`, `arrive_before` — `HH:MM`, local to the flight's own timezone
- `min_duration_minutes`, `max_duration_minutes`

Sorting:
- `sort_by` — one of `price_asc`, `price_desc`, `duration_asc`, `duration_desc`,
  `departure_time`, `arrival_time`, `best_value` (default)

`GET /health` — liveness check.

### Response shape

```json
{
  "search_criteria": { "origin": "CGK", "destination": "DPS", "departure_date": "2025-12-15", "passengers": 1, "cabin_class": "economy" },
  "metadata": {
    "total_results": 13,
    "providers_queried": 4,
    "providers_succeeded": 4,
    "providers_failed": 0,
    "search_time_ms": 235,
    "cache_hit": false
  },
  "flights": [
    {
      "id": "QZ7250_AirAsia",
      "provider": "AirAsia",
      "airline": { "name": "AirAsia", "code": "QZ" },
      "flight_number": "QZ7250",
      "departure": { "airport": "CGK", "city": "Jakarta", "datetime": "2025-12-15T15:15:00+07:00", "timestamp": 1734246900 },
      "arrival": { "airport": "DPS", "city": "Denpasar", "datetime": "2025-12-15T20:35:00+08:00", "timestamp": 1734267300 },
      "duration": { "total_minutes": 260, "formatted": "4h 20m" },
      "stops": 1,
      "stop_airports": ["SOC"],
      "price": { "amount": 485000, "currency": "IDR", "formatted": "Rp 485.000" },
      "available_seats": 88,
      "cabin_class": "economy",
      "aircraft": null,
      "amenities": [],
      "baggage": { "carry_on": "Cabin baggage only, checked bags additional fee" },
      "price_comparison": [ { "provider": "AirAsia", "amount": 485000, "currency": "IDR", "formatted": "Rp 485.000" } ],
      "best_value_score": 87.4
    }
  ]
}
```

`price_comparison` is only populated when the same physical flight (same
flight number, route and departure instant) was quoted by more than one
provider — see "Cross-provider merging" below.

## Architecture

```
cmd/server          entrypoint: wires config -> providers -> cache -> aggregator -> HTTP handlers
cmd/mockserver       serves input/*.json fixtures as HTTP endpoints

internal/
  models             normalized Flight/SearchParams/FilterParams/SearchResponse schema
  providers          one adapter per airline: raw JSON -> models.Flight
  timeutil           datetime/duration format parsing shared by all adapters
  currency           renders Price.Amount as a human-readable string (e.g. "Rp 650.000")
  validator          schedule/price/required-field sanity checks
  ratelimit          per-provider token-bucket rate limiter
  retry              exponential-backoff retry helper for transient provider failures
  aggregator         concurrent fan-out to providers + cross-provider dedup/merge
  cache              in-memory TTL cache of aggregated (pre-filter) results
  filter             price/stops/airline/time-window/duration filtering
  ranking            best-value scoring (price + duration + stops, normalized)
  sortutil           final ordering
  handlers/httpserver  HTTP layer: query parsing, routing, logging, recovery
  mockapi            the mock airline HTTP server used by cmd/mockserver
```

Request flow: `handlers.SearchHandler` parses the query string, calls
`aggregator.Search` (which checks the cache, or fans out to all four
providers concurrently with per-provider timeouts and merges the results),
applies `filter.Apply`, computes `ranking.Compute` scores, and finally
`sortutil.Apply`s the requested ordering.

## Handling real-world data problems

The four fixtures in `input/` were deliberately inconsistent, and the
adapters (`internal/providers/*.go`) exist specifically to absorb that:

- **Timestamp formats.** AirAsia and Garuda use RFC3339 with a colon in the
  offset (`+07:00`); Batik Air omits the colon (`+0700`); Lion Air gives a
  naive local clock string plus a separate IANA zone name
  (`"2025-12-15T05:30:00"` + `"Asia/Jakarta"`). `internal/timeutil` handles
  all three so every flight ends up as an absolute, comparable `time.Time`.

- **Duration is never trusted from the provider, always derived.** Every
  adapter computes `duration.total_minutes` as
  `arrivalInstant.Sub(departureInstant)`, ignoring whatever duration/travel-time
  field the provider supplied. This matters concretely: Garuda's `GA315`
  fixture has a two-segment itinerary where segment 2's own
  `duration_minutes` (90) is wrong once you account for the WIB→WITA
  timezone change (the real segment-2 flight time is 30 minutes); trusting
  it would overstate the total trip by an hour. Deriving duration from
  timestamps sidesteps that class of bug entirely. (Verified in
  `internal/providers/garuda_test.go`.)

- **A provider's own top-level fields can be stale.** That same `GA315`
  record says `"stops": 0` and `"arrival": {"airport": "SUB", ...}` at the
  top level, but its `segments` array shows it actually continues on to
  DPS. The Garuda adapter treats the segment chain as ground truth whenever
  it's present: final destination, stop count, and stop airports are all
  recomputed from segments rather than copied from the (misleading)
  top-level fields.

- **Missing optional fields.** AirAsia never supplies an aircraft model
  (serialized as `null`, not omitted); Garuda's third flight omits
  `amenities`; various providers omit stop/segment/connection arrays for
  direct flights. Adapters default these to `nil`-safe zero values (`[]`
  for lists) rather than propagating `nil` panics or inconsistent shapes.

- **Schedule validation.** `internal/validator.ValidateSchedule` rejects any
  flight whose arrival instant isn't strictly after departure — checked on
  absolute instants, not local clock strings, since two airports 2 hours
  apart by clock time can be in different timezones (comparing `"08:15" >
  "05:30"` as strings would happen to work here but is not what the
  correct comparison actually depends on). A rejected flight is dropped
  and logged; it does not fail the whole provider or request.

- **Partial provider failure.** Each provider call runs in its own
  goroutine with its own overall timeout (`internal/aggregator.Aggregator.Search`);
  a provider whose retries are exhausted, times out, or returns malformed
  JSON is recorded in `metadata.provider_errors` and excluded, while the
  others still return results. Use the mock server's `?fail=true` or
  `?malformed=true` query params (see `internal/mockapi/server.go`) to see
  this in action.

## Cross-provider merging

Real aggregators often see the same physical flight quoted by more than one
source (the airline's own API and an OTA reselling the same inventory).
`internal/aggregator/merge.go` groups normalized flights by
`(flight_number, departure_airport, arrival_airport, departure_datetime)`;
within a group, the lowest price wins as the displayed entry, and every
provider's quote is attached as `price_comparison`. The four sample fixtures
don't happen to overlap, so this mostly exercises as groups of one in the
demo data — `internal/aggregator/merge_test.go` covers the actual merge
behavior with synthetic duplicates.

## Currency display

Every `Price` (and each `price_comparison` entry) carries both the raw
`amount`/`currency` for programmatic use and a `formatted` field for display,
e.g. `1250000` + `"IDR"` → `"Rp 1.250.000"`. `internal/currency.Format`
applies each currency's real-world display convention (symbol, thousands
separator, decimal places) rather than one hardcoded style — IDR is shown as
whole rupiah with `.` as the thousands separator and no decimal places,
matching how it's actually quoted, while an unrecognized currency code falls
back to `"<CODE> 1,234,567.00"` so display never breaks for a currency this
package doesn't know about yet. Formatting always happens in
`internal/providers/common.go`'s `buildFlight` — the same single choke point
that derives duration — so every provider gets it for free.

## Best-value ranking

`internal/ranking.Compute` min-max normalizes price, duration, and stop
count across the *current* filtered result set (so the score is meaningful
regardless of route or absolute price level), then combines them as a
weighted sum (`DefaultWeights`: 50% price, 30% duration, 20% stops) into a
0–100 score. This is what `sort_by=best_value` (the default) sorts on.

## Caching

`internal/cache` holds the merged, normalized (pre-filter) result set per
`(origin, destination, date, cabin_class, passengers)` key for
`CACHE_TTL_SECONDS` (default 180s) — short, because prices and seat
availability change frequently. Filtering, ranking, and sorting are always
applied fresh on top of a cache hit, so query params like `sort_by` or
`max_price` never need to be part of the cache key.

## Rate limiting and retry

Every provider call goes through `internal/providers.resilientClient`
(`internal/providers/http.go`), which wraps the raw HTTP GET with two
independent concerns:

- **Rate limiting** (`internal/ratelimit`). Each provider gets its own
  token-bucket limiter — `PROVIDER_RATE_LIMIT_RPS` requests/second on
  average, with bursts up to `PROVIDER_RATE_LIMIT_BURST` — so a spike of
  concurrent user searches can't hammer one airline's API past whatever
  quota it allows. Limiters are per-provider, not shared, because one
  airline's rate limit (or outage) is independent of another's.

- **Retry with exponential backoff** (`internal/retry`). A failed attempt is
  retried up to `PROVIDER_RETRY_MAX_ATTEMPTS` times total, waiting
  `PROVIDER_RETRY_BASE_DELAY_MS` before the first retry and doubling (capped
  at `PROVIDER_RETRY_MAX_DELAY_MS`) each time after, with jitter so multiple
  providers backing off at once don't retry in lockstep. Not every failure is
  retried: `internal/providers/http.go` classifies a definitive 4xx response
  or an unparseable endpoint URL as non-retryable (retrying the exact same
  bad request just wastes attempts), while network errors, JSON-decoding
  hiccups, 5xx, and 429 responses are treated as transient and retried.

These two are deliberately separate from the aggregator's own per-provider
timeout: `PROVIDER_TIMEOUT_MS` bounds the *entire* `Provider.Search` call
(every attempt plus every backoff wait combined) via the context passed into
it, while `PROVIDER_HTTP_TIMEOUT_MS` (the `http.Client`'s own `Timeout`)
bounds a *single* attempt. Keeping the per-attempt timeout well under the
overall timeout is what lets a hung request fail fast enough to leave room
for the next retry instead of a single stuck attempt silently swallowing the
whole budget.

See it in action: point one provider at the mock server's failure-simulation
endpoint and watch the other three still return results while that one
reports a `provider_errors` entry after exhausting its retries —
```
AIRASIA_API_URL="http://localhost:8081/airasia/search?fail=true" go run ./cmd/server
```
— or check `internal/providers/resilience_test.go` and
`internal/ratelimit`/`internal/retry`'s own test suites for the behavior in
isolation.

## Configuration

All via environment variables, all optional:

| Variable | Default | Meaning |
|---|---|---|
| `PORT` | `8080` | API server port |
| `MOCK_API_BASE_URL` | `http://localhost:8081` | base URL used to derive default provider URLs |
| `AIRASIA_API_URL` / `BATIKAIR_API_URL` / `GARUDA_API_URL` / `LIONAIR_API_URL` | derived from `MOCK_API_BASE_URL` | per-provider endpoint override, e.g. to point at a real airline API |
| `PROVIDER_TIMEOUT_MS` | `5000` | overall timeout for one provider call, including all retries and backoff waits |
| `PROVIDER_HTTP_TIMEOUT_MS` | `2000` | timeout for a single HTTP attempt (must stay below `PROVIDER_TIMEOUT_MS`) |
| `PROVIDER_RATE_LIMIT_RPS` | `5` | requests/second allowed per provider (token-bucket average rate) |
| `PROVIDER_RATE_LIMIT_BURST` | `5` | burst size per provider's rate limiter |
| `PROVIDER_RETRY_MAX_ATTEMPTS` | `3` | total attempts per provider call, including the first |
| `PROVIDER_RETRY_BASE_DELAY_MS` | `200` | delay before the first retry; doubles each subsequent retry |
| `PROVIDER_RETRY_MAX_DELAY_MS` | `2000` | cap on the backoff delay regardless of attempt count |
| `CACHE_TTL_SECONDS` | `180` | aggregated result cache lifetime |
| `MOCK_SERVER_PORT` | `8081` | mock server port (cmd/mockserver only) |
| `FIXTURES_DIR` | `input` | directory the mock server reads fixtures from |

`price_comparison` is only populated when the same physical flight (same
flight number, route and departure instant) was quoted by more than one
provider — see "Cross-provider merging" below.
