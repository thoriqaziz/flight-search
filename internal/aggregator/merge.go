package aggregator

import (
	"sort"

	"flight-search/internal/models"
)

func dedupeKey(f models.Flight) string {
	return f.FlightNumber + "|" + f.Departure.Airport + "|" + f.Arrival.Airport + "|" + f.Departure.DateTime
}

func mergeDuplicates(flights []models.Flight) []models.Flight {
	groups := make(map[string][]models.Flight)
	order := make([]string, 0, len(flights))
	for _, f := range flights {
		k := dedupeKey(f)
		if _, seen := groups[k]; !seen {
			order = append(order, k)
		}
		groups[k] = append(groups[k], f)
	}

	merged := make([]models.Flight, 0, len(order))
	for _, k := range order {
		group := groups[k]
		if len(group) == 1 {
			merged = append(merged, group[0])
			continue
		}

		sort.SliceStable(group, func(i, j int) bool {
			return group[i].Price.Amount < group[j].Price.Amount
		})

		best := group[0]
		comparisons := make([]models.ProviderPrice, 0, len(group))
		for _, g := range group {
			comparisons = append(comparisons, models.ProviderPrice{
				Provider:  g.Provider,
				Amount:    g.Price.Amount,
				Currency:  g.Price.Currency,
				Formatted: g.Price.Formatted,
			})
		}
		best.PriceComparison = comparisons
		merged = append(merged, best)
	}
	return merged
}
