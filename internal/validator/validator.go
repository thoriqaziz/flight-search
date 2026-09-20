package validator

import (
	"fmt"
	"time"
)

func ValidateSchedule(flightNumber string, departure, arrival time.Time) error {
	if !arrival.After(departure) {
		return fmt.Errorf("flight %s has invalid schedule: arrival (%s) is not after departure (%s)",
			flightNumber, arrival.Format(time.RFC3339), departure.Format(time.RFC3339))
	}
	return nil
}

func ValidatePrice(flightNumber string, amount float64) error {
	if amount <= 0 {
		return fmt.Errorf("flight %s has invalid price: %v", flightNumber, amount)
	}
	return nil
}

func RequireNonEmpty(flightNumber, field, value string) error {
	if value == "" {
		return fmt.Errorf("flight %s is missing required field %q", flightNumber, field)
	}
	return nil
}
