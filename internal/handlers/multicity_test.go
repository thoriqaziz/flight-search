package handlers

import (
	"net/http"
	"testing"

	"flight-search/internal/models"
)

func TestServeHTTP_MultiCityPopulatesEverySegment(t *testing.T) {
	h := newTestHandler(500000)
	resp, body := doSearch(t, h,
		"origin=CGK&origin=DPS&origin=SUB&"+
			"destination=DPS&destination=SUB&destination=CGK&"+
			"date=2025-12-15&date=2025-12-18&date=2025-12-22")

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if body.SearchCriteria.TripType != models.TripMultiCity {
		t.Errorf("expected trip_type %q, got %q", models.TripMultiCity, body.SearchCriteria.TripType)
	}
	if len(body.SearchCriteria.Segments) != 3 {
		t.Fatalf("expected 3 segments echoed in search_criteria, got %d", len(body.SearchCriteria.Segments))
	}
	if body.SearchCriteria.Origin != "" || body.SearchCriteria.Destination != "" || body.SearchCriteria.DepartureDate != "" {
		t.Error("multi-city response should leave the single-leg origin/destination/departure_date fields empty")
	}
	if body.Flights != nil || body.OutboundFlights != nil || body.ReturnFlights != nil {
		t.Error("multi-city response should not populate the one-way/round-trip flight fields")
	}

	if len(body.Segments) != 3 {
		t.Fatalf("expected 3 segment results, got %d", len(body.Segments))
	}

	wantRoutes := [][2]string{{"CGK", "DPS"}, {"DPS", "SUB"}, {"SUB", "CGK"}}
	for i, seg := range body.Segments {
		if len(seg.Flights) != 1 {
			t.Fatalf("segment %d: expected 1 flight, got %d", i, len(seg.Flights))
		}
		f := seg.Flights[0]
		if f.Departure.Airport != wantRoutes[i][0] || f.Arrival.Airport != wantRoutes[i][1] {
			t.Errorf("segment %d: expected route %s->%s, got %s->%s", i, wantRoutes[i][0], wantRoutes[i][1], f.Departure.Airport, f.Arrival.Airport)
		}
		if seg.Metadata.ProvidersSucceeded != 1 {
			t.Errorf("segment %d: expected 1 provider to succeed, got %d", i, seg.Metadata.ProvidersSucceeded)
		}
	}

	// Metadata is the roll-up across all 3 segments (1 flight, 1 provider
	// queried/succeeded each).
	if body.Metadata.TotalResults != 3 {
		t.Errorf("expected combined total_results 3, got %d", body.Metadata.TotalResults)
	}
	if body.Metadata.ProvidersQueried != 3 || body.Metadata.ProvidersSucceeded != 3 {
		t.Errorf("expected combined providers_queried/succeeded 3/3, got %d/%d", body.Metadata.ProvidersQueried, body.Metadata.ProvidersSucceeded)
	}

	if body.MultiCityEstimate == nil {
		t.Fatal("expected a MultiCityEstimate when every segment has results")
	}
	if len(body.MultiCityEstimate.CheapestFlightIDs) != 3 {
		t.Errorf("expected 3 cheapest_flight_ids, got %d", len(body.MultiCityEstimate.CheapestFlightIDs))
	}
	if body.MultiCityEstimate.TotalPrice.Amount != 1500000 {
		t.Errorf("expected combined total price 1500000 (3x500000), got %v", body.MultiCityEstimate.TotalPrice.Amount)
	}
	if body.MultiCityEstimate.TotalPrice.Formatted != "Rp 1.500.000" {
		t.Errorf("expected formatted total \"Rp 1.500.000\", got %q", body.MultiCityEstimate.TotalPrice.Formatted)
	}
}

func TestServeHTTP_MultiCityMismatchedArrayLengthsRejected(t *testing.T) {
	h := newTestHandler(500000)
	resp, _ := doSearch(t, h,
		"origin=CGK&origin=DPS&"+
			"destination=DPS&destination=SUB&"+
			"date=2025-12-15") // only 1 date for 2 origins/destinations
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for mismatched segment array lengths, got %d", resp.StatusCode)
	}
}

func TestServeHTTP_MultiCityTooManySegmentsRejected(t *testing.T) {
	h := newTestHandler(500000)
	q := ""
	for range maxMultiCitySegments + 1 {
		q += "origin=CGK&destination=DPS&date=2025-12-15&"
	}
	resp, _ := doSearch(t, h, q)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for exceeding max segment count, got %d", resp.StatusCode)
	}
}

func TestServeHTTP_MultiCitySegmentDateOutOfOrderRejected(t *testing.T) {
	h := newTestHandler(500000)
	resp, _ := doSearch(t, h,
		"origin=CGK&origin=DPS&"+
			"destination=DPS&destination=SUB&"+
			"date=2025-12-15&date=2025-12-10") // segment 2 date before segment 1
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for out-of-order segment dates, got %d", resp.StatusCode)
	}
}

func TestServeHTTP_MultiCityRejectsReturnDate(t *testing.T) {
	h := newTestHandler(500000)
	resp, _ := doSearch(t, h,
		"origin=CGK&origin=DPS&"+
			"destination=DPS&destination=SUB&"+
			"date=2025-12-15&date=2025-12-18&"+
			"return_date=2025-12-20")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 when combining multi-city params with return_date, got %d", resp.StatusCode)
	}
}

func TestServeHTTP_MultiCityInvalidDateFormatRejected(t *testing.T) {
	h := newTestHandler(500000)
	resp, _ := doSearch(t, h,
		"origin=CGK&origin=DPS&"+
			"destination=DPS&destination=SUB&"+
			"date=2025-12-15&date=18-12-2025")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed segment date, got %d", resp.StatusCode)
	}
}
