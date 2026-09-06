package enterprise

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNormalizeAmountPreservesTenDecimalPlaces(t *testing.T) {
	amount, err := NormalizeAmount("1234567890.1234567891")
	require.NoError(t, err)
	require.Equal(t, "1234567890.1234567891", amount)
}

func TestNormalizeAmountRejectsHugeScientificExponentQuickly(t *testing.T) {
	started := time.Now()
	_, err := NormalizeAmount("1e100000000")
	require.ErrorIs(t, err, ErrInvalidAmount)
	require.Less(t, time.Since(started), 100*time.Millisecond)
}

func TestAllocationRepositoryRejectsBlankActorAndReasonBeforeDatabaseAccess(t *testing.T) {
	repo := NewRepository(nil)
	_, err := repo.CreateAllocation(context.Background(), CreateAllocationParams{
		Amount:   "1",
		Reason:   "reason",
		ActorRef: "  ",
	})
	require.True(t, errors.Is(err, ErrActorRefRequired))

	_, err = repo.ReviseAllocation(context.Background(), ReviseAllocationParams{
		Amount:   "1",
		Reason:   "  ",
		ActorRef: "actor",
	})
	require.True(t, errors.Is(err, ErrReasonRequired))
}

func TestNormalizeAmountRejectsNegativeOrExcessPrecision(t *testing.T) {
	for _, value := range []string{"-0.0000000001", "1.00000000001", "not-a-number"} {
		_, err := NormalizeAmount(value)
		require.Error(t, err, value)
	}
}
