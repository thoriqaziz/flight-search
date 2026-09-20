package ranking

import "flight-search/internal/models"

type Weights struct {
	Price    float64
	Duration float64
	Stops    float64
}

func DefaultWeights() Weights {
	return Weights{Price: 0.5, Duration: 0.3, Stops: 0.2}
}

func Compute(flights []models.Flight, w Weights) {
	if len(flights) == 0 {
		return
	}

	minPrice, maxPrice := flights[0].Price.Amount, flights[0].Price.Amount
	minDur, maxDur := flights[0].Duration.TotalMinutes, flights[0].Duration.TotalMinutes
	maxStops := flights[0].Stops
	for _, f := range flights {
		if f.Price.Amount < minPrice {
			minPrice = f.Price.Amount
		}
		if f.Price.Amount > maxPrice {
			maxPrice = f.Price.Amount
		}
		if f.Duration.TotalMinutes < minDur {
			minDur = f.Duration.TotalMinutes
		}
		if f.Duration.TotalMinutes > maxDur {
			maxDur = f.Duration.TotalMinutes
		}
		if f.Stops > maxStops {
			maxStops = f.Stops
		}
	}

	for i := range flights {
		priceScore := 1.0
		if maxPrice > minPrice {
			priceScore = (maxPrice - flights[i].Price.Amount) / (maxPrice - minPrice)
		}
		durScore := 1.0
		if maxDur > minDur {
			durScore = float64(maxDur-flights[i].Duration.TotalMinutes) / float64(maxDur-minDur)
		}
		stopsScore := 1.0
		if maxStops > 0 {
			stopsScore = float64(maxStops-flights[i].Stops) / float64(maxStops)
		}
		score := w.Price*priceScore + w.Duration*durScore + w.Stops*stopsScore
		flights[i].BestValueScore = round2(score * 100)
	}
}

// round2 rounds v to 2 decimal places.
func round2(v float64) float64 {
	return float64(int(v*100+0.5)) / 100
}
