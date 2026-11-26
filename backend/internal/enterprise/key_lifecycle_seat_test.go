package enterprise

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestEnforceEnterpriseSeatLimitCountsOnlyEffectiveKeys(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectBegin()
	tx, err := db.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	mock.ExpectQuery(`(?s)SELECT e\.seat_count.*api_key\.expires_at IS NULL OR api_key\.expires_at > statement_timestamp\(\).*GROUP BY e\.seat_count`).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"seat_count", "active_key_count"}).AddRow(2, 1))
	require.NoError(t, enforceEnterpriseSeatLimit(context.Background(), tx, 7))
	mock.ExpectRollback()
	require.NoError(t, tx.Rollback())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestEnforceEnterpriseSeatLimitRejectsAtCapacity(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectBegin()
	tx, err := db.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	mock.ExpectQuery(`(?s)SELECT e\.seat_count.*FROM enterprises AS e`).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"seat_count", "active_key_count"}).AddRow(1, 1))
	require.ErrorIs(t, enforceEnterpriseSeatLimit(context.Background(), tx, 7), ErrEnterpriseSeatLimit)
	mock.ExpectRollback()
	require.NoError(t, tx.Rollback())
	require.NoError(t, mock.ExpectationsWereMet())
}
