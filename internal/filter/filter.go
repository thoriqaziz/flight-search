package filter

import (
	"strings"
	"time"

	"flight-search/internal/models"
)

func Apply(flights []models.Flight, f models.FilterParams) []models.Flight {
	out := make([]models.Flight, 0, len(flights))
	for _, fl := range flights {
		if !matches(fl, f) {
			continue
		}
		out = append(out, fl)
	}
	return out
}

func matches(fl models.Flight, f models.FilterParams) bool {
	if f.MinPrice != nil && fl.Price.Amount < *f.MinPrice {
		return false
	}
	if f.MaxPrice != nil && fl.Price.Amount > *f.MaxPrice {
		return false
	}
	if f.MaxStops != nil && fl.Stops > *f.MaxStops {
		return false
	}
	if f.MinDurationMin != nil && fl.Duration.TotalMinutes < *f.MinDurationMin {
		return false
	}
	if f.MaxDurationMin != nil && fl.Duration.TotalMinutes > *f.MaxDurationMin {
		return false
	}
	if len(f.Airlines) > 0 && !matchesAirline(fl, f.Airlines) {
		return false
	}
	if f.DepartAfter != "" && timeOfDay(fl.DepartureTime) < f.DepartAfter {
		return false
	}
	if f.DepartBefore != "" && timeOfDay(fl.DepartureTime) > f.DepartBefore {
		return false
	}
	if f.ArriveAfter != "" && timeOfDay(fl.ArrivalTime) < f.ArriveAfter {
		return false
	}
	if f.ArriveBefore != "" && timeOfDay(fl.ArrivalTime) > f.ArriveBefore {
		return false
	}
	return true
}

func matchesAirline(fl models.Flight, airlines []string) bool {
	for _, a := range airlines {
		a = strings.TrimSpace(a)
		if strings.EqualFold(a, fl.Airline.Code) || strings.EqualFold(a, fl.Airline.Name) {
			return true
		}
	}
	return false
}

func timeOfDay(t time.Time) string {
	return t.Format("15:04")
}
