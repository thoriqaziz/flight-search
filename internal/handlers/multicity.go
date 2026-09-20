package handlers

import (
	"flight-search/internal/currency"
	"flight-search/internal/models"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// maxMultiCitySegments caps how many legs a single multi-city search can
// request, so a malformed or abusive query can't fan out to an unbounded
// number of concurrent aggregator searches.
const maxMultiCitySegments = 6

// isMultiCitySearch reports whether q carries more than one value for
// origin, destination, or date -- the signal that this is a multi-city
// search (one leg per position across the three parallel arrays) rather
// than the ordinary single-value one-way/round-trip case.
func isMultiCitySearch(q url.Values) bool {
	return len(q["origin"]) > 1 || len(q["destination"]) > 1 || len(q["date"]) > 1
}

// serveMultiCity handles a search where origin/destination/date were each
// given more than once, e.g.
// ?origin=CGK&origin=DPS&origin=SUB&destination=DPS&destination=SUB&destination=CGK&date=2025-12-15&date=2025-12-18&date=2025-12-22
// describing a CGK->DPS->SUB->CGK itinerary. It shares passengers,
// cabin_class, filters, and sort order with the one-way/round-trip path,
// but validation and the response shape are specific to having an
// arbitrary number of independently-searched segments.
func (h *SearchHandler) serveMultiCity(w http.ResponseWriter, r *http.Request, q url.Values) {
	if strings.TrimSpace(q.Get("return_date")) != "" {
		writeError(w, http.StatusBadRequest, "return_date is not valid for a multi-city search; give each leg via repeated origin/destination/date instead")
		return
	}

	origins := q["origin"]
	destinations := q["destination"]
	dates := q["date"]

	if len(origins) != len(destinations) || len(origins) != len(dates) {
		writeError(w, http.StatusBadRequest, "origin, destination, and date must each be given the same number of times for a multi-city search")
		return
	}
	if len(origins) > maxMultiCitySegments {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("a multi-city search supports at most %d segments", maxMultiCitySegments))
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

	segmentCriteria := make([]models.SegmentCriteria, len(origins))
	segmentParams := make([]models.SearchParams, len(origins))

	var previousDate string
	for i := range origins {
		origin := strings.ToUpper(strings.TrimSpace(origins[i]))
		destination := strings.ToUpper(strings.TrimSpace(destinations[i]))
		date := strings.TrimSpace(dates[i])

		if origin == "" || destination == "" || date == "" {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("segment %d: origin, destination, and date are all required", i+1))
			return
		}
		if _, err := time.Parse("2006-01-02", date); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("segment %d: date must be in YYYY-MM-DD format", i+1))
			return
		}
		if i > 0 && date < previousDate {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("segment %d: date must not be before segment %d's date", i+1, i))
			return
		}
		previousDate = date

		segmentCriteria[i] = models.SegmentCriteria{Origin: origin, Destination: destination, Date: date}
		segmentParams[i] = models.SearchParams{
			Origin: origin, Destination: destination, Date: date,
			Passengers: passengers, CabinClass: cabinClass,
		}
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

	start := time.Now()
	segmentFlights, segmentMetas, err := h.searchLegs(r.Context(), segmentParams, bypassCache, fp, sortOpt)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	segments := make([]models.SegmentResult, len(segmentParams))
	for i, p := range segmentParams {
		segments[i] = models.SegmentResult{
			Origin: p.Origin, Destination: p.Destination, Date: p.Date,
			Metadata: segmentMetas[i], Flights: segmentFlights[i],
		}
	}

	meta := combinedMetadata(segmentMetas)
	meta.SearchTimeMs = time.Since(start).Milliseconds()

	writeJSON(w, http.StatusOK, models.SearchResponse{
		SearchCriteria: models.SearchCriteria{
			Segments:   segmentCriteria,
			TripType:   models.TripMultiCity,
			Passengers: passengers,
			CabinClass: cabinClass,
		},
		Metadata:          meta,
		Segments:          segments,
		MultiCityEstimate: buildMultiCityEstimate(segments),
	})
}

// combinedMetadata rolls up per-segment metadata into one summary: the
// count fields are summed across segments and every segment's
// ProviderErrors are concatenated (without re-attributing which segment
// each came from -- that detail is still available per-segment in
// SegmentResult.Metadata). CacheHit is true only if every segment was
// served from cache. SearchTimeMs is left at zero; the caller overwrites it
// with the actual wall-clock time of the whole (concurrent) multi-segment
// search, which is a more accurate number than anything derivable from the
// individual segments' own timings.
func combinedMetadata(segmentMeta []models.Metadata) models.Metadata {
	combined := models.Metadata{CacheHit: true}
	for _, m := range segmentMeta {
		combined.TotalResults += m.TotalResults
		combined.ProvidersQueried += m.ProvidersQueried
		combined.ProvidersSucceeded += m.ProvidersSucceeded
		combined.ProvidersFailed += m.ProvidersFailed
		combined.ProviderErrors = append(combined.ProviderErrors, m.ProviderErrors...)
		if !m.CacheHit {
			combined.CacheHit = false
		}
	}
	return combined
}

// buildMultiCityEstimate finds the cheapest flight in each segment --
// independent of whatever sort order the caller requested -- and reports
// the combined price a traveler booking exactly those flights would pay.
// It returns nil if any segment has zero results, or if the segments'
// cheapest flights aren't all priced in the same currency (summing
// mismatched currencies would be meaningless).
func buildMultiCityEstimate(segments []models.SegmentResult) *models.MultiCityEstimate {
	if len(segments) == 0 {
		return nil
	}

	ids := make([]string, len(segments))
	var total float64
	var curr string
	for i, seg := range segments {
		if len(seg.Flights) == 0 {
			return nil
		}
		cheapest := cheapestFlight(seg.Flights)
		if i == 0 {
			curr = cheapest.Price.Currency
		} else if cheapest.Price.Currency != curr {
			return nil
		}
		ids[i] = cheapest.ID
		total += cheapest.Price.Amount
	}

	return &models.MultiCityEstimate{
		CheapestFlightIDs: ids,
		TotalPrice: models.Price{
			Amount:    total,
			Currency:  curr,
			Formatted: currency.Format(total, curr),
		},
	}
}
