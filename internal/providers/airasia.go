package providers

import (
	"context"
	"flight-search/internal/models"
	"flight-search/internal/ratelimit"
	"flight-search/internal/retry"
	"flight-search/internal/timeutil"
	"log"
	"net/http"
)

type airasiaStop struct {
	Airport         string `json:"airport"`
	WaitTimeMinutes int    `json:"wait_time_minutes"`
}

type airasiaFlight struct {
	FlightCode    string        `json:"flight_code"`
	Airline       string        `json:"airline"`
	FromAirport   string        `json:"from_airport"`
	ToAirport     string        `json:"to_airport"`
	DepartTime    string        `json:"depart_time"`
	ArriveTime    string        `json:"arrive_time"`
	DurationHours float64       `json:"duration_hours"`
	DirectFlight  bool          `json:"direct_flight"`
	Stops         []airasiaStop `json:"stops,omitempty"`
	PriceIDR      float64       `json:"price_idr"`
	Seats         int           `json:"seats"`
	CabinClass    string        `json:"cabin_class"`
	BaggageNote   string        `json:"baggage_note"`
}

type airasiaResponse struct {
	Status  string          `json:"status"`
	Flights []airasiaFlight `json:"flights"`
}

type AirAsiaProvider struct {
	endpoint string
	client   *resilientClient
}

func NewAirAsiaProvider(endpoint string, client *http.Client, limiter *ratelimit.Limiter, retryCfg retry.Config) *AirAsiaProvider {
	return &AirAsiaProvider{endpoint: endpoint, client: newResilientClient(client, limiter, retryCfg)}
}

func (p *AirAsiaProvider) Name() string { return "AirAsia" }

func (p *AirAsiaProvider) Search(ctx context.Context, params models.SearchParams) ([]models.Flight, error) {
	var raw airasiaResponse
	if err := p.client.fetchJSON(ctx, p.endpoint, params, &raw); err != nil {
		return nil, err
	}

	flights := make([]models.Flight, 0, len(raw.Flights))
	for _, f := range raw.Flights {
		depTime, err := timeutil.ParseWithOffset(f.DepartTime)
		if err != nil {
			log.Printf("airasia: skipping %s: %v", f.FlightCode, err)
			continue
		}
		arrTime, err := timeutil.ParseWithOffset(f.ArriveTime)
		if err != nil {
			log.Printf("airasia: skipping %s: %v", f.FlightCode, err)
			continue
		}

		stops := 0
		stopAirports := []string{}
		if !f.DirectFlight {
			stops = len(f.Stops)
			if stops == 0 {
				stops = 1 // marked connecting but no detail supplied
			}
			for _, s := range f.Stops {
				stopAirports = append(stopAirports, s.Airport)
			}
		}

		flight, err := buildFlight(buildFlightParams{
			Provider:       p.Name(),
			FlightNumber:   f.FlightCode,
			Airline:        models.Airline{Name: f.Airline, Code: extractAirlineCode(f.FlightCode)},
			DepAirport:     f.FromAirport,
			DepTime:        depTime,
			ArrAirport:     f.ToAirport,
			ArrTime:        arrTime,
			Stops:          stops,
			StopAirports:   stopAirports,
			Price:          models.Price{Amount: f.PriceIDR, Currency: "IDR"},
			AvailableSeats: f.Seats,
			CabinClass:     f.CabinClass,
			Aircraft:       nil, // not supplied by this provider
			Amenities:      nil,
			Baggage:        models.Baggage{CarryOn: f.BaggageNote},
		})
		if err != nil {
			log.Printf("airasia: skipping %s: %v", f.FlightCode, err)
			continue
		}
		flights = append(flights, flight)
	}
	return flights, nil
}
