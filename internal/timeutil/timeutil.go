package timeutil

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

var offsetLayouts = []string{
	time.RFC3339,               // 2025-12-15T04:45:00+07:00
	"2006-01-02T15:04:05-0700", // 2025-12-15T07:15:00+0700
	"2006-01-02T15:04:05.999999999Z07:00",
}

func ParseWithOffset(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	var lastErr error
	for _, layout := range offsetLayouts {
		if t, err := time.Parse(layout, value); err == nil {
			return t, nil
		} else {
			lastErr = err
		}
	}
	return time.Time{}, fmt.Errorf("timeutil: unrecognized offset datetime %q: %w", value, lastErr)
}

func ParseWithZoneName(value, zoneName string) (time.Time, error) {
	value = strings.TrimSpace(value)
	loc, err := time.LoadLocation(zoneName)
	if err != nil {
		return time.Time{}, fmt.Errorf("timeutil: unknown timezone %q: %w", zoneName, err)
	}
	t, err := time.ParseInLocation("2006-01-02T15:04:05", value, loc)
	if err != nil {
		return time.Time{}, fmt.Errorf("timeutil: unrecognized local datetime %q: %w", value, err)
	}
	return t, nil
}

func ParseDurationString(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("timeutil: empty duration string")
	}
	var hours, minutes int
	var found bool
	var num strings.Builder
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			num.WriteRune(r)
		case r == 'h' || r == 'H':
			v, err := atoiOrZero(num.String())
			if err != nil {
				return 0, fmt.Errorf("timeutil: invalid duration %q: %w", s, err)
			}
			hours = v
			found = true
			num.Reset()
		case r == 'm' || r == 'M':
			v, err := atoiOrZero(num.String())
			if err != nil {
				return 0, fmt.Errorf("timeutil: invalid duration %q: %w", s, err)
			}
			minutes = v
			found = true
			num.Reset()
		case r == ' ':
			// ignore separators
		default:
			// ignore any other decoration ("min", "hrs", etc.)
		}
	}
	if !found {
		return 0, fmt.Errorf("timeutil: no recognizable hours/minutes in %q", s)
	}
	return hours*60 + minutes, nil
}

func atoiOrZero(s string) (int, error) {
	if s == "" {
		return 0, nil
	}
	return strconv.Atoi(s)
}

func FormatMinutes(totalMinutes int) string {
	if totalMinutes < 0 {
		totalMinutes = 0
	}
	h := totalMinutes / 60
	m := totalMinutes % 60
	switch {
	case h > 0 && m > 0:
		return fmt.Sprintf("%dh %dm", h, m)
	case h > 0:
		return fmt.Sprintf("%dh", h)
	default:
		return fmt.Sprintf("%dm", m)
	}
}
