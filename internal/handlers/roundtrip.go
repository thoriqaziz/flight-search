package handlers

import (
	"flight-search/internal/currency"
	"flight-search/internal/models"
)

// buildRoundTripEstimate finds the cheapest outbound and cheapest return
// flight -- independent of whatever sort order the caller requested -- and
// reports the combined price a traveler booking exactly those two flights
// would pay. It returns nil if either leg has no results, or if the
// cheapest flights on each leg are priced in different currencies (summing
// mismatched currencies would be meaningless; none of this project's mock
// providers do that, but a real one might).
func buildRoundTripEstimate(outbound, ret []models.Flight) *models.RoundTripEstimate {
	if len(outbound) == 0 || len(ret) == 0 {
		return nil
	}

	cheapestOutbound := cheapestFlight(outbound)
	cheapestReturn := cheapestFlight(ret)
	if cheapestOutbound.Price.Currency != cheapestReturn.Price.Currency {
		return nil
	}

	total := cheapestOutbound.Price.Amount + cheapestReturn.Price.Amount
	return &models.RoundTripEstimate{
		CheapestOutboundID: cheapestOutbound.ID,
		CheapestReturnID:   cheapestReturn.ID,
		TotalPrice: models.Price{
			Amount:    total,
			Currency:  cheapestOutbound.Price.Currency,
			Formatted: currency.Format(total, cheapestOutbound.Price.Currency),
		},
	}
}

// cheapestFlight returns the lowest-priced flight in a non-empty slice.
func cheapestFlight(flights []models.Flight) models.Flight {
	best := flights[0]
	for _, f := range flights[1:] {
		if f.Price.Amount < best.Price.Amount {
			best = f
		}
	}
	return best
}
