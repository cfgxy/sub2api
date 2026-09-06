package enterprise

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNormalizeAmountPreservesEightDecimalPlaces(t *testing.T) {
	amount, err := NormalizeAmount("123456789012.12345678")
	require.NoError(t, err)
	require.Equal(t, "123456789012.12345678", amount)
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
	for _, value := range []string{"-0.00000001", "1.000000001", "not-a-number"} {
		_, err := NormalizeAmount(value)
		require.Error(t, err, value)
	}
}

func TestNormalizeAmountRejectsNumericTwentyEightOverflow(t *testing.T) {
	for _, value := range []string{"1000000000000", "999999999999.999999999"} {
		_, err := NormalizeAmount(value)
		require.ErrorIs(t, err, ErrInvalidAmount, value)
	}
}

func TestNormalizeAmountCanonicalizesDecimalInputsWithoutFloatDrift(t *testing.T) {
	for input, expected := range map[string]string{
		"0.1":                   "0.10000000",
		"0.2":                   "0.20000000",
		"0.3":                   "0.30000000",
		"999999999999.99999999": "999999999999.99999999",
	} {
		amount, err := NormalizeAmount(input)
		require.NoError(t, err, input)
		require.Equal(t, expected, amount, input)
	}
}
