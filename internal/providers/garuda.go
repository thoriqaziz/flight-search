package providers

import (
	"context"
	"log"
	"net/http"
	"strconv"

	"flight-search/internal/models"
	"flight-search/internal/ratelimit"
	"flight-search/internal/retry"
	"flight-search/internal/timeutil"
)

type garudaLegPoint struct {
	Airport string `json:"airport"`
	Time    string `json:"time"`
}

type garudaSegment struct {
	FlightNumber    string         `json:"flight_number"`
	Departure       garudaLegPoint `json:"departure"`
	Arrival         garudaLegPoint `json:"arrival"`
	DurationMinutes int            `json:"duration_minutes"`
	LayoverMinutes  int            `json:"layover_minutes,omitempty"`
}

type garudaEndpoint struct {
	Airport  string `json:"airport"`
	City     string `json:"city"`
	Time     string `json:"time"`
	Terminal string `json:"terminal,omitempty"`
}

type garudaPrice struct {
	Amount   float64 `json:"amount"`
	Currency string  `json:"currency"`
}

type garudaBaggage struct {
	CarryOn int `json:"carry_on"`
	Checked int `json:"checked"`
}

type garudaFlight struct {
	FlightID        string          `json:"flight_id"`
	Airline         string          `json:"airline"`
	AirlineCode     string          `json:"airline_code"`
	Departure       garudaEndpoint  `json:"departure"`
	Arrival         garudaEndpoint  `json:"arrival"`
	DurationMinutes int             `json:"duration_minutes"`
	Stops           int             `json:"stops"`
	Aircraft        string          `json:"aircraft,omitempty"`
	Price           garudaPrice     `json:"price"`
	Segments        []garudaSegment `json:"segments,omitempty"`
	AvailableSeats  int             `json:"available_seats"`
	FareClass       string          `json:"fare_class"`
	Baggage         garudaBaggage   `json:"baggage"`
	Amenities       []string        `json:"amenities,omitempty"`
}

type garudaResponse struct {
	Status  string         `json:"status"`
	Flights []garudaFlight `json:"flights"`
}

type GarudaProvider struct {
	endpoint string
	client   *resilientClient
}

func NewGarudaProvider(endpoint string, client *http.Client, limiter *ratelimit.Limiter, retryCfg retry.Config) *GarudaProvider {
	return &GarudaProvider{endpoint: endpoint, client: newResilientClient(client, limiter, retryCfg)}
}

// Name returns the display name used in metadata and price comparisons.
func (p *GarudaProvider) Name() string { return "Garuda Indonesia" }

func (p *GarudaProvider) Search(ctx context.Context, params models.SearchParams) ([]models.Flight, error) {
	var raw garudaResponse
	if err := p.client.fetchJSON(ctx, p.endpoint, params, &raw); err != nil {
		return nil, err
	}

	flights := make([]models.Flight, 0, len(raw.Flights))
	for _, f := range raw.Flights {
		depAirport, depCity, depTimeStr := f.Departure.Airport, f.Departure.City, f.Departure.Time
		arrAirport, arrCity, arrTimeStr := f.Arrival.Airport, f.Arrival.City, f.Arrival.Time
		stops := f.Stops
		stopAirports := []string{}

		if len(f.Segments) > 1 {
			// Trust the segment chain over the (possibly stale) top-level
			// fields: real final destination is the last segment's arrival.
			last := f.Segments[len(f.Segments)-1]
			arrAirport = last.Arrival.Airport
			arrTimeStr = last.Arrival.Time
			// City isn't carried on segments; only reuse the top-level city
			// if it actually still matches the resolved arrival airport.
			if arrAirport != f.Arrival.Airport {
				arrCity = ""
			}
			stops = len(f.Segments) - 1
			for _, seg := range f.Segments[:len(f.Segments)-1] {
				stopAirports = append(stopAirports, seg.Arrival.Airport)
			}
		}

		depTime, err := timeutil.ParseWithOffset(depTimeStr)
		if err != nil {
			log.Printf("garuda: skipping %s: %v", f.FlightID, err)
			continue
		}
		arrTime, err := timeutil.ParseWithOffset(arrTimeStr)
		if err != nil {
			log.Printf("garuda: skipping %s: %v", f.FlightID, err)
			continue
		}

		var aircraft *string
		if f.Aircraft != "" {
			a := f.Aircraft
			aircraft = &a
		}

		flight, err := buildFlight(buildFlightParams{
			Provider:       p.Name(),
			FlightNumber:   f.FlightID,
			Airline:        models.Airline{Name: f.Airline, Code: f.AirlineCode},
			DepAirport:     depAirport,
			DepCity:        depCity,
			DepTime:        depTime,
			ArrAirport:     arrAirport,
			ArrCity:        arrCity,
			ArrTime:        arrTime,
			Stops:          stops,
			StopAirports:   stopAirports,
			Price:          models.Price{Amount: f.Price.Amount, Currency: f.Price.Currency},
			AvailableSeats: f.AvailableSeats,
			CabinClass:     f.FareClass,
			Aircraft:       aircraft,
			Amenities:      f.Amenities,
			Baggage:        formatGarudaBaggage(f.Baggage),
		})
		if err != nil {
			log.Printf("garuda: skipping %s: %v", f.FlightID, err)
			continue
		}
		flights = append(flights, flight)
	}
	return flights, nil
}

// formatGarudaBaggage converts Garuda's numeric piece counts into the
// free-form baggage text used by the normalized schema.
func formatGarudaBaggage(b garudaBaggage) models.Baggage {
	return models.Baggage{
		CarryOn: piecesLabel(b.CarryOn),
		Checked: piecesLabel(b.Checked),
	}
}

// piecesLabel renders a piece count as "N piece(s)", or "" for zero/negative
// counts (treated as "not specified").
func piecesLabel(n int) string {
	if n <= 0 {
		return ""
	}
	if n == 1 {
		return "1 piece"
	}
	return strconv.Itoa(n) + " pieces"
}
