package providers

import (
	"context"
	"log"
	"net/http"
	"strings"

	"flight-search/internal/models"
	"flight-search/internal/ratelimit"
	"flight-search/internal/retry"
	"flight-search/internal/timeutil"
)

// Batik Air wire format: nested under "results", offsets without a colon
// ("+0700"), fare broken into base/tax/total, connections as a separate
// array from numberOfStops.

type batikConnection struct {
	StopAirport  string `json:"stopAirport"`
	StopDuration string `json:"stopDuration"`
}

type batikFare struct {
	BasePrice    float64 `json:"basePrice"`
	Taxes        float64 `json:"taxes"`
	TotalPrice   float64 `json:"totalPrice"`
	CurrencyCode string  `json:"currencyCode"`
	Class        string  `json:"class"`
}

type batikFlight struct {
	FlightNumber      string            `json:"flightNumber"`
	AirlineName       string            `json:"airlineName"`
	AirlineIATA       string            `json:"airlineIATA"`
	Origin            string            `json:"origin"`
	Destination       string            `json:"destination"`
	DepartureDateTime string            `json:"departureDateTime"`
	ArrivalDateTime   string            `json:"arrivalDateTime"`
	TravelTime        string            `json:"travelTime"`
	NumberOfStops     int               `json:"numberOfStops"`
	Connections       []batikConnection `json:"connections,omitempty"`
	Fare              batikFare         `json:"fare"`
	SeatsAvailable    int               `json:"seatsAvailable"`
	AircraftModel     string            `json:"aircraftModel"`
	BaggageInfo       string            `json:"baggageInfo"`
	OnboardServices   []string          `json:"onboardServices"`
}

type batikResponse struct {
	Code    int           `json:"code"`
	Message string        `json:"message"`
	Results []batikFlight `json:"results"`
}

type BatikAirProvider struct {
	endpoint string
	client   *resilientClient
}

func NewBatikAirProvider(endpoint string, client *http.Client, limiter *ratelimit.Limiter, retryCfg retry.Config) *BatikAirProvider {
	return &BatikAirProvider{endpoint: endpoint, client: newResilientClient(client, limiter, retryCfg)}
}

// Name returns the display name used in metadata and price comparisons.
func (p *BatikAirProvider) Name() string { return "Batik Air" }

func (p *BatikAirProvider) Search(ctx context.Context, params models.SearchParams) ([]models.Flight, error) {
	var raw batikResponse
	if err := p.client.fetchJSON(ctx, p.endpoint, params, &raw); err != nil {
		return nil, err
	}
	if raw.Code != 0 && raw.Code != 200 {
		return nil, &providerAPIError{provider: p.Name(), message: raw.Message}
	}

	flights := make([]models.Flight, 0, len(raw.Results))
	for _, f := range raw.Results {
		depTime, err := timeutil.ParseWithOffset(f.DepartureDateTime)
		if err != nil {
			log.Printf("batikair: skipping %s: %v", f.FlightNumber, err)
			continue
		}
		arrTime, err := timeutil.ParseWithOffset(f.ArrivalDateTime)
		if err != nil {
			log.Printf("batikair: skipping %s: %v", f.FlightNumber, err)
			continue
		}

		stopAirports := make([]string, 0, len(f.Connections))
		for _, c := range f.Connections {
			stopAirports = append(stopAirports, c.StopAirport)
		}

		aircraft := f.AircraftModel
		carryOn, checked := splitBaggageInfo(f.BaggageInfo)

		flight, err := buildFlight(buildFlightParams{
			Provider:       p.Name(),
			FlightNumber:   f.FlightNumber,
			Airline:        models.Airline{Name: f.AirlineName, Code: f.AirlineIATA},
			DepAirport:     f.Origin,
			DepTime:        depTime,
			ArrAirport:     f.Destination,
			ArrTime:        arrTime,
			Stops:          f.NumberOfStops,
			StopAirports:   stopAirports,
			Price:          models.Price{Amount: f.Fare.TotalPrice, Currency: f.Fare.CurrencyCode},
			AvailableSeats: f.SeatsAvailable,
			CabinClass:     mapBatikFareClass(f.Fare.Class),
			Aircraft:       &aircraft,
			Amenities:      f.OnboardServices,
			Baggage:        models.Baggage{CarryOn: carryOn, Checked: checked},
		})
		if err != nil {
			log.Printf("batikair: skipping %s: %v", f.FlightNumber, err)
			continue
		}
		flights = append(flights, flight)
	}
	return flights, nil
}

// splitBaggageInfo turns "7kg cabin, 20kg checked" into ("7kg cabin", "20kg checked").
func splitBaggageInfo(info string) (carryOn, checked string) {
	parts := strings.SplitN(info, ",", 2)
	if len(parts) == 2 {
		return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	}
	return strings.TrimSpace(info), ""
}

func mapBatikFareClass(class string) string {
	switch strings.ToUpper(class) {
	case "Y", "ECONOMY":
		return "economy"
	case "C", "BUSINESS":
		return "business"
	case "F", "FIRST":
		return "first"
	default:
		return strings.ToLower(class)
	}
}

type providerAPIError struct {
	provider string
	message  string
}

func (e *providerAPIError) Error() string {
	return e.provider + " API error: " + e.message
}
