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
type SearchCriteria struct {
	Origin        string `json:"origin"`
	Destination   string `json:"destination"`
	DepartureDate string `json:"departure_date"`
	Passengers    int    `json:"passengers"`
	CabinClass    string `json:"cabin_class"`
}

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

// SearchResponse is the full JSON body returned by GET /api/v1/search.
type SearchResponse struct {
	SearchCriteria SearchCriteria `json:"search_criteria"`
	Metadata       Metadata       `json:"metadata"`
	Flights        []Flight       `json:"flights"`
}
