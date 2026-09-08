package enterprise

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAllocationWindowValidation(t *testing.T) {
	anchor := time.Date(2026, 9, 8, 10, 0, 0, 0, time.UTC)

	for _, windowType := range []string{WindowTypeDay, WindowTypeWeek, WindowTypeMonth} {
		require.NoError(t, ValidateAllocationWindow(windowType, anchor))
	}
	for _, windowType := range []string{"5h", "7d", "daily", "weekly", "monthly"} {
		require.ErrorIs(t, ValidateAllocationWindow(windowType, anchor), ErrInvalidWindowType)
	}
	require.ErrorIs(t, ValidateAllocationWindow(WindowTypeDay, time.Time{}), ErrInvalidWindowAnchor)
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
