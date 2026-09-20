package handlers

import (
	"context"
	"flight-search/internal/aggregator"
	"flight-search/internal/filter"
	"flight-search/internal/models"
	"flight-search/internal/ranking"
	"flight-search/internal/sortutil"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type SearchHandler struct {
	Aggregator *aggregator.Aggregator
	Weights    ranking.Weights
}

// ServeHTTP handles GET /api/v1/search: it validates the required
// origin/destination/date query params (plus an optional return_date for a  round trip),
// runs the aggregated provider search for each leg (optionally bypassing the cache),
// then applies filters, computes best-value scores, sorts the result, and writes the final JSON response.
// Filters and sort order apply identically to both legs of a round trip
// -- there is no per-leg override.
func (h *SearchHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "only GET is supported")
		return
	}

	q := r.URL.Query()

	origin := strings.ToUpper(strings.TrimSpace(q.Get("origin")))
	destination := strings.ToUpper(strings.TrimSpace(q.Get("destination")))
	date := strings.TrimSpace(q.Get("date"))

	if origin == "" || destination == "" || date == "" {
		writeError(w, http.StatusBadRequest, "origin, destination, and date are required query parameters")
		return
	}
	if _, err := time.Parse("2006-01-02", date); err != nil {
		writeError(w, http.StatusBadRequest, "date must be in YYYY-MM-DD format")
		return
	}

	returnDate := strings.TrimSpace(q.Get("return_date"))
	isRoundTrip := returnDate != ""
	if isRoundTrip {
		if _, err := time.Parse("2006-01-02", returnDate); err != nil {
			writeError(w, http.StatusBadRequest, "return_date must be in YYYY-MM-DD format")
			return
		}
		if returnDate < date {
			writeError(w, http.StatusBadRequest, "return_date must not be before date")
			return
		}
	}

	passengers := 1
	if v := q.Get("passengers"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			passengers = n
		} else {
			writeError(w, http.StatusBadRequest, "passengers must be a positive integer")
			return
		}
	}

	cabinClass := strings.ToLower(strings.TrimSpace(q.Get("cabin_class")))
	if cabinClass == "" {
		cabinClass = "economy"
	}

	bypassCache := parseBool(q.Get("refresh")) || parseBool(q.Get("no_cache"))

	fp, err := parseFilterParams(q)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	sortOpt := models.SortOption(strings.ToLower(strings.TrimSpace(q.Get("sort_by"))))
	if sortOpt == "" {
		sortOpt = models.SortBestValue
	}

	criteria := models.SearchCriteria{
		Origin:        origin,
		Destination:   destination,
		DepartureDate: date,
		Passengers:    passengers,
		CabinClass:    cabinClass,
		TripType:      models.TripOneWay,
	}

	outboundParams := models.SearchParams{
		Origin:      origin,
		Destination: destination,
		Date:        date,
		Passengers:  passengers,
		CabinClass:  cabinClass,
	}

	if !isRoundTrip {
		flights, meta, err := h.searchLeg(r.Context(), outboundParams, bypassCache, fp, sortOpt)
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, models.SearchResponse{
			SearchCriteria: criteria,
			Metadata:       meta,
			Flights:        flights,
		})
		return
	}

	// The return leg simply flies the route in reverse on the return date;
	// everything else (passengers, cabin, filters, sort) is shared.
	returnParams := models.SearchParams{
		Origin:      destination,
		Destination: origin,
		Date:        returnDate,
		Passengers:  passengers,
		CabinClass:  cabinClass,
	}

	outboundFlights, outboundMeta, returnFlights, returnMeta, err := h.searchBothLegs(r.Context(), outboundParams, returnParams, bypassCache, fp, sortOpt)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	criteria.ReturnDate = returnDate
	criteria.TripType = models.TripRoundTrip

	writeJSON(w, http.StatusOK, models.SearchResponse{
		SearchCriteria:    criteria,
		Metadata:          outboundMeta,
		ReturnMetadata:    &returnMeta,
		OutboundFlights:   outboundFlights,
		ReturnFlights:     returnFlights,
		RoundTripEstimate: buildRoundTripEstimate(outboundFlights, returnFlights),
	})
}

// searchLeg runs one leg's aggregated search, then applies filtering,
// best-value ranking, and sorting to it. The returned slice is never nil
// (an empty result is []models.Flight{}) so it marshals as "[]" rather than
// "null".
func (h *SearchHandler) searchLeg(ctx context.Context, params models.SearchParams, bypassCache bool, fp models.FilterParams, sortOpt models.SortOption) ([]models.Flight, models.Metadata, error) {
	flights, meta, err := h.Aggregator.Search(ctx, params, bypassCache)
	if err != nil {
		return nil, models.Metadata{}, err
	}

	flights = filter.Apply(flights, fp)
	ranking.Compute(flights, h.Weights)
	sortutil.Apply(flights, sortOpt)

	meta.TotalResults = len(flights)
	if flights == nil {
		flights = []models.Flight{}
	}
	return flights, meta, nil
}

// searchBothLegs runs the outbound and return legs of a round trip
// concurrently (each leg already fans out to all providers internally, so
// running the two legs in parallel roughly halves total latency compared to
// running them one after another) and returns as soon as both are done. If
// either leg fails, its error is returned; the other leg's goroutine is
// still allowed to finish before this function returns.
func (h *SearchHandler) searchBothLegs(ctx context.Context, outboundParams, returnParams models.SearchParams, bypassCache bool, fp models.FilterParams, sortOpt models.SortOption) (outbound []models.Flight, outboundMeta models.Metadata, ret []models.Flight, returnMeta models.Metadata, err error) {
	var outboundErr, returnErr error

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		outbound, outboundMeta, outboundErr = h.searchLeg(ctx, outboundParams, bypassCache, fp, sortOpt)
	}()
	go func() {
		defer wg.Done()
		ret, returnMeta, returnErr = h.searchLeg(ctx, returnParams, bypassCache, fp, sortOpt)
	}()
	wg.Wait()

	if outboundErr != nil {
		return nil, models.Metadata{}, nil, models.Metadata{}, outboundErr
	}
	if returnErr != nil {
		return nil, models.Metadata{}, nil, models.Metadata{}, returnErr
	}
	return outbound, outboundMeta, ret, returnMeta, nil
}

func parseFilterParams(q url.Values) (models.FilterParams, error) {
	fp := models.FilterParams{
		DepartAfter:  q.Get("depart_after"),
		DepartBefore: q.Get("depart_before"),
		ArriveAfter:  q.Get("arrive_after"),
		ArriveBefore: q.Get("arrive_before"),
	}

	var err error
	if fp.MinPrice, err = parseFloatPtr(q.Get("min_price")); err != nil {
		return fp, fmt.Errorf("min_price: %w", err)
	}
	if fp.MaxPrice, err = parseFloatPtr(q.Get("max_price")); err != nil {
		return fp, fmt.Errorf("max_price: %w", err)
	}
	if fp.MaxStops, err = parseIntPtr(q.Get("max_stops")); err != nil {
		return fp, fmt.Errorf("max_stops: %w", err)
	}
	if fp.MinDurationMin, err = parseIntPtr(q.Get("min_duration_minutes")); err != nil {
		return fp, fmt.Errorf("min_duration_minutes: %w", err)
	}
	if fp.MaxDurationMin, err = parseIntPtr(q.Get("max_duration_minutes")); err != nil {
		return fp, fmt.Errorf("max_duration_minutes: %w", err)
	}
	if a := strings.TrimSpace(q.Get("airlines")); a != "" {
		fp.Airlines = strings.Split(a, ",")
	}
	return fp, nil
}

func parseFloatPtr(v string) (*float64, error) {
	if v == "" {
		return nil, nil
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return nil, err
	}
	return &f, nil
}

func parseIntPtr(v string) (*int, error) {
	if v == "" {
		return nil, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return nil, err
	}
	return &n, nil
}

func parseBool(v string) bool {
	b, _ := strconv.ParseBool(v)
	return b
}
