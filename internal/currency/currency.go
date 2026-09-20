package currency

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

type spec struct {
	symbol      string
	thousandSep byte
	decimalSep  byte
	decimals    int
}

var known = map[string]spec{
	"IDR": {symbol: "Rp ", thousandSep: '.', decimalSep: ',', decimals: 0},
	"USD": {symbol: "$", thousandSep: ',', decimalSep: '.', decimals: 2},
	"SGD": {symbol: "S$", thousandSep: ',', decimalSep: '.', decimals: 2},
	"MYR": {symbol: "RM ", thousandSep: ',', decimalSep: '.', decimals: 2},
}

func Format(amount float64, currencyCode string) string {
	code := strings.ToUpper(strings.TrimSpace(currencyCode))
	s, ok := known[code]
	if !ok {
		s = spec{symbol: code + " ", thousandSep: ',', decimalSep: '.', decimals: 2}
	}

	neg := amount < 0
	number := formatNumber(math.Abs(amount), s.thousandSep, s.decimalSep, s.decimals)
	if neg {
		return "-" + s.symbol + number
	}
	return s.symbol + number
}

func formatNumber(amount float64, thousandSep, decimalSep byte, decimals int) string {
	scale := math.Pow10(decimals)
	rounded := math.Round(amount * scale)
	whole := int64(rounded / scale)
	fraction := int64(rounded) - whole*int64(scale)

	out := groupThousands(strconv.FormatInt(whole, 10), thousandSep)
	if decimals > 0 {
		out += string(decimalSep) + fmt.Sprintf("%0*d", decimals, fraction)
	}
	return out
}

func groupThousands(digits string, sep byte) string {
	n := len(digits)
	if n <= 3 {
		return digits
	}

	firstGroup := n % 3
	if firstGroup == 0 {
		firstGroup = 3
	}

	var b strings.Builder
	b.Grow(n + n/3)
	b.WriteString(digits[:firstGroup])
	for i := firstGroup; i < n; i += 3 {
		b.WriteByte(sep)
		b.WriteString(digits[i : i+3])
	}
	return b.String()
}
