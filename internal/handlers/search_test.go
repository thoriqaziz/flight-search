package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"flight-search/internal/aggregator"
	"flight-search/internal/cache"
	"flight-search/internal/models"
	"flight-search/internal/providers"
	"flight-search/internal/ranking"
)

// echoStubProvider is a fake Provider for handler tests: it returns exactly
// one flight per call, flying whatever origin/destination/date was actually
// requested, so the same stub works for both an outbound and a return leg
// without any test-side bookkeeping about which leg is being searched.
type echoStubProvider struct {
	priceIDR float64
}

func (echoStubProvider) Name() string { return "Stub" }

func (p echoStubProvider) Search(ctx context.Context, params models.SearchParams) ([]models.Flight, error) {
	day, err := time.Parse("2006-01-02", params.Date)
	if err != nil {
		return nil, err
	}
	loc := time.FixedZone("WIB", 7*3600)
	dep := time.Date(day.Year(), day.Month(), day.Day(), 8, 0, 0, 0, loc)
	arr := dep.Add(2 * time.Hour)

	return []models.Flight{
		{
			ID:           "ST100_Stub_" + params.Origin + params.Destination,
			Provider:     "Stub",
			Airline:      models.Airline{Name: "StubAir", Code: "ST"},
			FlightNumber: "ST100",
			Departure: models.Endpoint{
				Airport: params.Origin, DateTime: dep.Format(time.RFC3339), Timestamp: dep.Unix(),
			},
			Arrival: models.Endpoint{
				Airport: params.Destination, DateTime: arr.Format(time.RFC3339), Timestamp: arr.Unix(),
			},
			Duration:       models.Duration{TotalMinutes: 120, Formatted: "2h"},
			Price:          models.Price{Amount: p.priceIDR, Currency: "IDR", Formatted: "Rp"},
			AvailableSeats: 10,
			Amenities:      []string{},
			DepartureTime:  dep,
			ArrivalTime:    arr,
		},
	}, nil
}

func newTestHandler(price float64) *SearchHandler {
	agg := aggregator.New([]providers.Provider{echoStubProvider{priceIDR: price}}, cache.New(time.Minute), time.Second)
	return &SearchHandler{Aggregator: agg, Weights: ranking.DefaultWeights()}
}

func doSearch(t *testing.T, h *SearchHandler, query string) (*http.Response, models.SearchResponse) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/search?"+query, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	resp := rec.Result()

	var body models.SearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decoding response body: %v", err)
	}
	return resp, body
}

func TestServeHTTP_OneWayUnchangedShape(t *testing.T) {
	h := newTestHandler(500000)
	resp, body := doSearch(t, h, "origin=CGK&destination=DPS&date=2025-12-15")

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if body.SearchCriteria.TripType != models.TripOneWay {
		t.Errorf("expected trip_type %q, got %q", models.TripOneWay, body.SearchCriteria.TripType)
	}
	if len(body.Flights) != 1 {
		t.Fatalf("expected 1 flight in Flights, got %d", len(body.Flights))
	}
	if body.OutboundFlights != nil || body.ReturnFlights != nil {
		t.Error("one-way response should not populate OutboundFlights/ReturnFlights")
	}
	if body.ReturnMetadata != nil {
		t.Error("one-way response should not populate ReturnMetadata")
	}
	if body.RoundTripEstimate != nil {
		t.Error("one-way response should not populate RoundTripEstimate")
	}
}

func TestServeHTTP_RoundTripPopulatesBothLegs(t *testing.T) {
	h := newTestHandler(500000)
	resp, body := doSearch(t, h, "origin=CGK&destination=DPS&date=2025-12-15&return_date=2025-12-20")

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if body.SearchCriteria.TripType != models.TripRoundTrip {
		t.Errorf("expected trip_type %q, got %q", models.TripRoundTrip, body.SearchCriteria.TripType)
	}
	if body.SearchCriteria.ReturnDate != "2025-12-20" {
		t.Errorf("expected return_date echoed back, got %q", body.SearchCriteria.ReturnDate)
	}
	if body.Flights != nil {
		t.Error("round-trip response should not populate the one-way Flights field")
	}
	if len(body.OutboundFlights) != 1 {
		t.Fatalf("expected 1 outbound flight, got %d", len(body.OutboundFlights))
	}
	if len(body.ReturnFlights) != 1 {
		t.Fatalf("expected 1 return flight, got %d", len(body.ReturnFlights))
	}
	if body.OutboundFlights[0].Departure.Airport != "CGK" || body.OutboundFlights[0].Arrival.Airport != "DPS" {
		t.Errorf("outbound flight has wrong route: %s -> %s", body.OutboundFlights[0].Departure.Airport, body.OutboundFlights[0].Arrival.Airport)
	}
	if body.ReturnFlights[0].Departure.Airport != "DPS" || body.ReturnFlights[0].Arrival.Airport != "CGK" {
		t.Errorf("return flight has wrong route: %s -> %s", body.ReturnFlights[0].Departure.Airport, body.ReturnFlights[0].Arrival.Airport)
	}
	if body.ReturnMetadata == nil {
		t.Fatal("expected ReturnMetadata to be populated for a round trip")
	}

	if body.RoundTripEstimate == nil {
		t.Fatal("expected a RoundTripEstimate for a round trip with results on both legs")
	}
	if body.RoundTripEstimate.TotalPrice.Amount != 1000000 {
		t.Errorf("expected combined total price 1000000 (500000+500000), got %v", body.RoundTripEstimate.TotalPrice.Amount)
	}
	if body.RoundTripEstimate.TotalPrice.Formatted != "Rp 1.000.000" {
		t.Errorf("expected formatted total \"Rp 1.000.000\", got %q", body.RoundTripEstimate.TotalPrice.Formatted)
	}
}

func TestServeHTTP_ReturnDateBeforeDepartureIsRejected(t *testing.T) {
	h := newTestHandler(500000)
	resp, _ := doSearch(t, h, "origin=CGK&destination=DPS&date=2025-12-15&return_date=2025-12-10")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for return_date before date, got %d", resp.StatusCode)
	}
}

func TestServeHTTP_InvalidReturnDateFormatIsRejected(t *testing.T) {
	h := newTestHandler(500000)
	resp, _ := doSearch(t, h, "origin=CGK&destination=DPS&date=2025-12-15&return_date=20-12-2025")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed return_date, got %d", resp.StatusCode)
	}
}

func TestServeHTTP_SameDayRoundTripIsAllowed(t *testing.T) {
	h := newTestHandler(500000)
	resp, body := doSearch(t, h, "origin=CGK&destination=DPS&date=2025-12-15&return_date=2025-12-15")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for a same-day round trip, got %d", resp.StatusCode)
	}
	if body.SearchCriteria.TripType != models.TripRoundTrip {
		t.Errorf("expected trip_type %q, got %q", models.TripRoundTrip, body.SearchCriteria.TripType)
	}
}
