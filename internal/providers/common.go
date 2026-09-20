package providers

import (
	"flight-search/internal/currency"
	"flight-search/internal/models"
	"flight-search/internal/timeutil"
	"flight-search/internal/validator"
	"time"
)

type buildFlightParams struct {
	Provider       string
	FlightNumber   string
	Airline        models.Airline
	DepAirport     string
	DepCity        string
	DepTime        time.Time
	ArrAirport     string
	ArrCity        string
	ArrTime        time.Time
	Stops          int
	StopAirports   []string
	Price          models.Price
	AvailableSeats int
	CabinClass     string
	Aircraft       *string
	Amenities      []string
	Baggage        models.Baggage
}

func buildFlight(p buildFlightParams) (models.Flight, error) {
	if err := validator.RequireNonEmpty(p.FlightNumber, "flight_number", p.FlightNumber); err != nil {
		return models.Flight{}, err
	}
	if err := validator.ValidateSchedule(p.FlightNumber, p.DepTime, p.ArrTime); err != nil {
		return models.Flight{}, err
	}
	if err := validator.ValidatePrice(p.FlightNumber, p.Price.Amount); err != nil {
		return models.Flight{}, err
	}

	durationMinutes := int(p.ArrTime.Sub(p.DepTime).Minutes())

	amenities := p.Amenities
	if amenities == nil {
		amenities = []string{}
	}
	stopAirports := p.StopAirports
	if stopAirports == nil {
		stopAirports = []string{}
	}

	price := p.Price
	price.Formatted = currency.Format(price.Amount, price.Currency)

	return models.Flight{
		ID:           p.FlightNumber + "_" + p.Provider,
		Provider:     p.Provider,
		Airline:      p.Airline,
		FlightNumber: p.FlightNumber,
		Departure: models.Endpoint{
			Airport:   p.DepAirport,
			City:      p.DepCity,
			DateTime:  p.DepTime.Format(time.RFC3339),
			Timestamp: p.DepTime.Unix(),
		},
		Arrival: models.Endpoint{
			Airport:   p.ArrAirport,
			City:      p.ArrCity,
			DateTime:  p.ArrTime.Format(time.RFC3339),
			Timestamp: p.ArrTime.Unix(),
		},
		Duration: models.Duration{
			TotalMinutes: durationMinutes,
			Formatted:    timeutil.FormatMinutes(durationMinutes),
		},
		Stops:          p.Stops,
		StopAirports:   stopAirports,
		Price:          price,
		AvailableSeats: p.AvailableSeats,
		CabinClass:     p.CabinClass,
		Aircraft:       p.Aircraft,
		Amenities:      amenities,
		Baggage:        p.Baggage,
		DepartureTime:  p.DepTime,
		ArrivalTime:    p.ArrTime,
	}, nil
}

func extractAirlineCode(flightNumber string) string {
	i := 0
	for i < len(flightNumber) && (flightNumber[i] < '0' || flightNumber[i] > '9') {
		i++
	}
	return flightNumber[:i]
}
