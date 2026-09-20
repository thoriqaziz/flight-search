package models

type SearchParams struct {
	Origin      string
	Destination string
	Date        string // YYYY-MM-DD
	Passengers  int
	CabinClass  string
}

type FilterParams struct {
	MinPrice       *float64
	MaxPrice       *float64
	MaxStops       *int
	Airlines       []string // matched case-insensitively against name or IATA code
	DepartAfter    string   // "HH:MM", local to the flight's own departure timezone
	DepartBefore   string
	ArriveAfter    string
	ArriveBefore   string
	MinDurationMin *int
	MaxDurationMin *int
}

// SortOption selects the ordering applied to a filtered result set.
type SortOption string

const (
	SortPriceAsc      SortOption = "price_asc"
	SortPriceDesc     SortOption = "price_desc"
	SortDurationAsc   SortOption = "duration_asc"
	SortDurationDesc  SortOption = "duration_desc"
	SortDepartureTime SortOption = "departure_time"
	SortArrivalTime   SortOption = "arrival_time"
	SortBestValue     SortOption = "best_value"
)

// SearchCriteria echoes back the parsed request in the response envelope.
// ReturnDate is only set for a round trip, in which case TripType is "round_trip"; otherwise TripType is "one_way".
type SearchCriteria struct {
	Origin        string `json:"origin"`
	Destination   string `json:"destination"`
	DepartureDate string `json:"departure_date"`
	ReturnDate    string `json:"return_date,omitempty"`
	TripType      string `json:"trip_type"`
	Passengers    int    `json:"passengers"`
	CabinClass    string `json:"cabin_class"`
}

// TripType values for SearchCriteria.TripType.
const (
	TripOneWay    = "one_way"
	TripRoundTrip = "round_trip"
)

// ProviderError captures a single provider's failure without failing the
// whole search -- partial results beat no results.
type ProviderError struct {
	Provider string `json:"provider"`
	Error    string `json:"error"`
}

// Metadata reports how the aggregation went: which providers answered, how
// long it took, and whether the result came from cache.
type Metadata struct {
	TotalResults       int             `json:"total_results"`
	ProvidersQueried   int             `json:"providers_queried"`
	ProvidersSucceeded int             `json:"providers_succeeded"`
	ProvidersFailed    int             `json:"providers_failed"`
	ProviderErrors     []ProviderError `json:"provider_errors,omitempty"`
	SearchTimeMs       int64           `json:"search_time_ms"`
	CacheHit           bool            `json:"cache_hit"`
}

// RoundTripEstimate is a display convenience for a round trip: the cheapest outbound flight and cheapest return flight combined,
// independent of whichever sort order the caller requested. It never affects which flights are returned
// or how OutboundFlights/ReturnFlights are ordered
// -- a traveler is still free to pick any outbound with any return.
type RoundTripEstimate struct {
	CheapestOutboundID string `json:"cheapest_outbound_id"`
	CheapestReturnID   string `json:"cheapest_return_id"`
	TotalPrice         Price  `json:"total_price"`
}

// SearchResponse is the full JSON body returned by GET /api/v1/search.
//
// A one-way search (TripType == TripOneWay) populates Flights and Metadata;
// OutboundFlights/ReturnFlights are left as null, and ReturnMetadata/
// RoundTripEstimate are omitted (they're pointers, so nil is omitted).
//
// A round trip (TripType == TripRoundTrip) populates OutboundFlights (with
// Metadata describing that leg's aggregation) and ReturnFlights (with
// ReturnMetadata describing that leg's); Flights is left as null. The two
// legs are searched, filtered, ranked and sorted independently of each
// other -- see RoundTripEstimate for the one piece of cross-leg information
// provided.
//
// Flights, OutboundFlights and ReturnFlights deliberately have no
// `omitempty`: encoding/json's omitempty treats a zero-length slice the same
// as a nil one, so it would drop a leg that's legitimately present but
// genuinely has zero results -- indistinguishable from "not applicable for
// this trip type". The field that IS applicable for the trip type is always
// an array (`[]` at worst); the field that ISN'T is `null`.
type SearchResponse struct {
	SearchCriteria SearchCriteria `json:"search_criteria"`
	Metadata       Metadata       `json:"metadata"`

	Flights []Flight `json:"flights"`

	ReturnMetadata    *Metadata          `json:"return_metadata,omitempty"`
	OutboundFlights   []Flight           `json:"outbound_flights"`
	ReturnFlights     []Flight           `json:"return_flights"`
	RoundTripEstimate *RoundTripEstimate `json:"round_trip_estimate,omitempty"`
}
