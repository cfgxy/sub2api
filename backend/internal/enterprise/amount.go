package enterprise

import (
	"errors"
	"strings"

	"github.com/shopspring/decimal"
)

var ErrInvalidAmount = errors.New("invalid enterprise amount")

func NormalizeAmount(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 64 || strings.ContainsAny(value, "eE") {
		return "", ErrInvalidAmount
	}
	amount, err := decimal.NewFromString(value)
	if err != nil || amount.IsNegative() || amount.Exponent() < -8 {
		return "", ErrInvalidAmount
	}

	normalized := amount.StringFixed(8)
	integerDigits := strings.TrimLeft(strings.SplitN(normalized, ".", 2)[0], "0")
	if len(integerDigits) > 12 {
		return "", ErrInvalidAmount
	}
	return normalized, nil
}
