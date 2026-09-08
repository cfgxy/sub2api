package enterprise

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAllocationWindowValidation(t *testing.T) {
	anchor := time.Date(2026, 9, 8, 10, 0, 0, 0, time.UTC)

	require.NoError(t, ValidateAllocationWindow(WindowType5h, anchor))
	require.NoError(t, ValidateAllocationWindow(WindowType7d, anchor))
	require.ErrorIs(t, ValidateAllocationWindow("1d", anchor), ErrInvalidWindowType)
	require.ErrorIs(t, ValidateAllocationWindow(WindowType5h, time.Time{}), ErrInvalidWindowAnchor)
}

func TestNormalizeAllocationReasonRejectsSensitiveOrUnboundedText(t *testing.T) {
	reason, err := NormalizeAllocationReason(" rebalance capacity ")
	require.NoError(t, err)
	require.Equal(t, "rebalance capacity", reason)

	for _, value := range []string{
		"", "Bearer secret", "token=secret", "password=secret", "cookie=secret",
		"sk-sensitive", "contact user@example.com", strings.Repeat("a", MaxAllocationReasonLength+1),
	} {
		_, err = NormalizeAllocationReason(value)
		require.Error(t, err, value)
	}
}
