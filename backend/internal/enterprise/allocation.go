package enterprise

import (
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"
)

const MaxAllocationReasonLength = 200

var (
	ErrUnsafeReason = errors.New("enterprise allocation reason contains sensitive data")
	emailPattern    = regexp.MustCompile(`(?i)\b[a-z0-9._%+-]+@[a-z0-9.-]+\.[a-z]{2,}\b`)
)

func NormalizeAllocationReason(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", ErrReasonRequired
	}
	if utf8.RuneCountInString(value) > MaxAllocationReasonLength {
		return "", ErrUnsafeReason
	}
	lower := strings.ToLower(value)
	for _, marker := range []string{"bearer ", "token=", "password=", "cookie=", "sk-"} {
		if strings.Contains(lower, marker) {
			return "", ErrUnsafeReason
		}
	}
	if emailPattern.MatchString(value) {
		return "", ErrUnsafeReason
	}
	return value, nil
}
