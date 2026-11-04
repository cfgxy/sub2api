package repository

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestRecordRealtimeBillingOutcomeUsesBillingTransaction(t *testing.T) {
	ctx := context.Background()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectBegin()
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	mock.ExpectExec(`(?s)INSERT INTO realtime_billing_outcomes`).
		WithArgs("call-hash", "response-id", int64(42)).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	err = recordRealtimeBillingOutcome(ctx, tx, &service.UsageBillingCommand{
		RealtimeCallHash: "call-hash",
		RequestID:        "response-id",
		APIKeyID:         42,
	})
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestHasRealtimeBillingOutcome(t *testing.T) {
	ctx := context.Background()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectQuery(`(?s)SELECT EXISTS \(`).
		WithArgs("call-hash").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	store := &usageBillingRepository{db: db}
	exists, err := store.HasRealtimeBillingOutcome(ctx, " call-hash ")
	require.NoError(t, err)
	require.True(t, exists)
	require.NoError(t, mock.ExpectationsWereMet())

	exists, err = store.HasRealtimeBillingOutcome(ctx, " ")
	require.NoError(t, err)
	require.False(t, exists)
}
