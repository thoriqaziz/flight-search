package sortutil

import (
	"sort"

	"flight-search/internal/models"
)

func Apply(flights []models.Flight, option models.SortOption) {
	switch option {
	case models.SortPriceAsc:
		sort.SliceStable(flights, func(i, j int) bool { return flights[i].Price.Amount < flights[j].Price.Amount })
	case models.SortPriceDesc:
		sort.SliceStable(flights, func(i, j int) bool { return flights[i].Price.Amount > flights[j].Price.Amount })
	case models.SortDurationAsc:
		sort.SliceStable(flights, func(i, j int) bool {
			return flights[i].Duration.TotalMinutes < flights[j].Duration.TotalMinutes
		})
	case models.SortDurationDesc:
		sort.SliceStable(flights, func(i, j int) bool {
			return flights[i].Duration.TotalMinutes > flights[j].Duration.TotalMinutes
		})
	case models.SortDepartureTime:
		sort.SliceStable(flights, func(i, j int) bool {
			return flights[i].DepartureTime.Before(flights[j].DepartureTime)
		})
	case models.SortArrivalTime:
		sort.SliceStable(flights, func(i, j int) bool {
			return flights[i].ArrivalTime.Before(flights[j].ArrivalTime)
		})
	default: // models.SortBestValue and anything unrecognized
		sort.SliceStable(flights, func(i, j int) bool {
			return flights[i].BestValueScore > flights[j].BestValueScore
		})
	}
}
