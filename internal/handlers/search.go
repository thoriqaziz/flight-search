package handlers

import (
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
	"time"
)

type SearchHandler struct {
	Aggregator *aggregator.Aggregator
	Weights    ranking.Weights
}

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

	params := models.SearchParams{
		Origin:      origin,
		Destination: destination,
		Date:        date,
		Passengers:  passengers,
		CabinClass:  cabinClass,
	}

	bypassCache := parseBool(q.Get("refresh")) || parseBool(q.Get("no_cache"))

	flights, meta, err := h.Aggregator.Search(r.Context(), params, bypassCache)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	fp, err := parseFilterParams(q)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	flights = filter.Apply(flights, fp)

	ranking.Compute(flights, h.Weights)

	sortOpt := models.SortOption(strings.ToLower(strings.TrimSpace(q.Get("sort_by"))))
	if sortOpt == "" {
		sortOpt = models.SortBestValue
	}
	sortutil.Apply(flights, sortOpt)

	meta.TotalResults = len(flights)
	if flights == nil {
		flights = []models.Flight{}
	}

	resp := models.SearchResponse{
		SearchCriteria: models.SearchCriteria{
			Origin:        origin,
			Destination:   destination,
			DepartureDate: date,
			Passengers:    passengers,
			CabinClass:    cabinClass,
		},
		Metadata: meta,
		Flights:  flights,
	}

	writeJSON(w, http.StatusOK, resp)
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
