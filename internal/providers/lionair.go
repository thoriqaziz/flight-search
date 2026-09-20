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

type lionCarrier struct {
	Name string `json:"name"`
	IATA string `json:"iata"`
}

type lionAirportInfo struct {
	Code string `json:"code"`
	Name string `json:"name"`
	City string `json:"city"`
}

type lionRoute struct {
	From lionAirportInfo `json:"from"`
	To   lionAirportInfo `json:"to"`
}

type lionSchedule struct {
	Departure         string `json:"departure"`
	DepartureTimezone string `json:"departure_timezone"`
	Arrival           string `json:"arrival"`
	ArrivalTimezone   string `json:"arrival_timezone"`
}

type lionLayover struct {
	Airport         string `json:"airport"`
	DurationMinutes int    `json:"duration_minutes"`
}

type lionPricing struct {
	Total    float64 `json:"total"`
	Currency string  `json:"currency"`
	FareType string  `json:"fare_type"`
}

type lionBaggageAllowance struct {
	Cabin string `json:"cabin"`
	Hold  string `json:"hold"`
}

type lionServices struct {
	WifiAvailable    bool                 `json:"wifi_available"`
	MealsIncluded    bool                 `json:"meals_included"`
	BaggageAllowance lionBaggageAllowance `json:"baggage_allowance"`
}

type lionFlight struct {
	ID         string        `json:"id"`
	Carrier    lionCarrier   `json:"carrier"`
	Route      lionRoute     `json:"route"`
	Schedule   lionSchedule  `json:"schedule"`
	FlightTime int           `json:"flight_time"`
	IsDirect   bool          `json:"is_direct"`
	StopCount  int           `json:"stop_count,omitempty"`
	Layovers   []lionLayover `json:"layovers,omitempty"`
	Pricing    lionPricing   `json:"pricing"`
	SeatsLeft  int           `json:"seats_left"`
	PlaneType  string        `json:"plane_type"`
	Services   lionServices  `json:"services"`
}

type lionResponse struct {
	Success bool `json:"success"`
	Data    struct {
		AvailableFlights []lionFlight `json:"available_flights"`
	} `json:"data"`
}

type LionAirProvider struct {
	endpoint string
	client   *resilientClient
}

func NewLionAirProvider(endpoint string, client *http.Client, limiter *ratelimit.Limiter, retryCfg retry.Config) *LionAirProvider {
	return &LionAirProvider{endpoint: endpoint, client: newResilientClient(client, limiter, retryCfg)}
}

// Name returns the display name used in metadata and price comparisons.
func (p *LionAirProvider) Name() string { return "Lion Air" }

func (p *LionAirProvider) Search(ctx context.Context, params models.SearchParams) ([]models.Flight, error) {
	var raw lionResponse
	if err := p.client.fetchJSON(ctx, p.endpoint, params, &raw); err != nil {
		return nil, err
	}
	if !raw.Success {
		return nil, &providerAPIError{provider: p.Name(), message: "response reported success=false"}
	}

	flights := make([]models.Flight, 0, len(raw.Data.AvailableFlights))
	for _, f := range raw.Data.AvailableFlights {
		depTime, err := timeutil.ParseWithZoneName(f.Schedule.Departure, f.Schedule.DepartureTimezone)
		if err != nil {
			log.Printf("lionair: skipping %s: %v", f.ID, err)
			continue
		}
		arrTime, err := timeutil.ParseWithZoneName(f.Schedule.Arrival, f.Schedule.ArrivalTimezone)
		if err != nil {
			log.Printf("lionair: skipping %s: %v", f.ID, err)
			continue
		}

		stops := 0
		stopAirports := []string{}
		if !f.IsDirect {
			stops = f.StopCount
			if stops == 0 {
				stops = len(f.Layovers)
			}
			for _, l := range f.Layovers {
				stopAirports = append(stopAirports, l.Airport)
			}
		}

		aircraft := f.PlaneType
		amenities := []string{}
		if f.Services.WifiAvailable {
			amenities = append(amenities, "wifi")
		}
		if f.Services.MealsIncluded {
			amenities = append(amenities, "meal")
		}

		flight, err := buildFlight(buildFlightParams{
			Provider:       p.Name(),
			FlightNumber:   f.ID,
			Airline:        models.Airline{Name: f.Carrier.Name, Code: f.Carrier.IATA},
			DepAirport:     f.Route.From.Code,
			DepCity:        f.Route.From.City,
			DepTime:        depTime,
			ArrAirport:     f.Route.To.Code,
			ArrCity:        f.Route.To.City,
			ArrTime:        arrTime,
			Stops:          stops,
			StopAirports:   stopAirports,
			Price:          models.Price{Amount: f.Pricing.Total, Currency: f.Pricing.Currency},
			AvailableSeats: f.SeatsLeft,
			CabinClass:     strings.ToLower(f.Pricing.FareType),
			Aircraft:       &aircraft,
			Amenities:      amenities,
			Baggage: models.Baggage{
				CarryOn: f.Services.BaggageAllowance.Cabin,
				Checked: f.Services.BaggageAllowance.Hold,
			},
		})
		if err != nil {
			log.Printf("lionair: skipping %s: %v", f.ID, err)
			continue
		}
		flights = append(flights, flight)
	}
	return flights, nil
}
