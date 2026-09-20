package models

import "time"

type Airline struct {
	Name string `json:"name"`
	Code string `json:"code"`
}

type Endpoint struct {
	Airport   string `json:"airport"`
	City      string `json:"city,omitempty"`
	DateTime  string `json:"datetime"`
	Timestamp int64  `json:"timestamp"`
}

type Price struct {
	Amount    float64 `json:"amount"`
	Currency  string  `json:"currency"`
	Formatted string  `json:"formatted"`
}

type Duration struct {
	TotalMinutes int    `json:"total_minutes"`
	Formatted    string `json:"formatted"`
}

type Baggage struct {
	CarryOn string `json:"carry_on,omitempty"`
	Checked string `json:"checked,omitempty"`
}

type ProviderPrice struct {
	Provider  string  `json:"provider"`
	Amount    float64 `json:"amount"`
	Currency  string  `json:"currency"`
	Formatted string  `json:"formatted"`
}

type Flight struct {
	ID              string          `json:"id"`
	Provider        string          `json:"provider"`
	Airline         Airline         `json:"airline"`
	FlightNumber    string          `json:"flight_number"`
	Departure       Endpoint        `json:"departure"`
	Arrival         Endpoint        `json:"arrival"`
	Duration        Duration        `json:"duration"`
	Stops           int             `json:"stops"`
	StopAirports    []string        `json:"stop_airports,omitempty"`
	Price           Price           `json:"price"`
	AvailableSeats  int             `json:"available_seats"`
	CabinClass      string          `json:"cabin_class,omitempty"`
	Aircraft        *string         `json:"aircraft"`
	Amenities       []string        `json:"amenities"`
	Baggage         Baggage         `json:"baggage"`
	PriceComparison []ProviderPrice `json:"price_comparison,omitempty"`
	BestValueScore  float64         `json:"best_value_score"`

	DepartureTime time.Time `json:"-"`
	ArrivalTime   time.Time `json:"-"`
}
