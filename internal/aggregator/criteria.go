package aggregator

import (
	"strings"

	"flight-search/internal/models"
)

func matchesCriteria(f models.Flight, params models.SearchParams) bool {
	if params.Origin != "" && !strings.EqualFold(f.Departure.Airport, params.Origin) {
		return false
	}
	if params.Destination != "" && !strings.EqualFold(f.Arrival.Airport, params.Destination) {
		return false
	}
	if params.Date != "" && f.DepartureTime.Format("2006-01-02") != params.Date {
		return false
	}
	return true
}

func filterByCriteria(flights []models.Flight, params models.SearchParams) []models.Flight {
	out := make([]models.Flight, 0, len(flights))
	for _, f := range flights {
		if matchesCriteria(f, params) {
			out = append(out, f)
		}
	}
	return out
}
