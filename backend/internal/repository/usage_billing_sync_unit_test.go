//go:build unit

package repository

import (
	"context"
	"database/sql"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func expectEnterpriseBillingAssignment(mock sqlmock.Sqlmock) {
	mock.ExpectQuery(billingKeyAssignmentSQL).WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"enterprise_id", "employee_id"}).AddRow(11, 21))
	mock.ExpectQuery(`(?s)SELECT id\s+FROM enterprise_employees\s+WHERE enterprise_id = \$1 AND id = \$2\s+FOR UPDATE`).
		WithArgs(int64(11), int64(21)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(21))
	mock.ExpectQuery(`(?s)SELECT api_key_id\s+FROM enterprise_key_assignments\s+WHERE enterprise_id = \$1 AND employee_id = \$2.*FOR UPDATE`).
		WithArgs(int64(11), int64(21)).
		WillReturnRows(sqlmock.NewRows([]string{"api_key_id"}).AddRow(8))
}

func TestApplyUsageBillingEffects_RotationChargesSuccessorCounters(t *testing.T) {
	ctx := context.Background()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectBegin()
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	expectEnterpriseBillingAssignment(mock)
	mock.ExpectQuery(billingKeyLockSQL).WithArgs(int64(8)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(8))
	mock.ExpectQuery(apiKeyQuotaIncrementSQL).
		WithArgs(2.0, int64(8), service.StatusAPIKeyActive, service.StatusAPIKeyQuotaExhausted).
		WillReturnRows(sqlmock.NewRows([]string{"exhausted"}).AddRow(false))
	mock.ExpectExec(apiKeyRateLimitIncrementSQL).WithArgs(2.0, int64(8)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	result := &service.UsageBillingApplyResult{}
	err = (&usageBillingRepository{}).applyUsageBillingEffects(ctx, tx, &service.UsageBillingCommand{
		APIKeyID: 7, APIKeyQuotaCost: 2, APIKeyRateLimitCost: 2,
	}, result)
	require.NoError(t, err)
	require.Equal(t, int64(8), result.BilledAPIKeyID)
	require.NoError(t, tx.Commit())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestApplyUsageBillingEffects_MissingEnterpriseSuccessorFails(t *testing.T) {
	ctx := context.Background()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectBegin()
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	expectEnterpriseBillingAssignment(mock)
	mock.ExpectQuery(billingKeyLockSQL).WithArgs(int64(8)).WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()
	err = (&usageBillingRepository{}).applyUsageBillingEffects(ctx, tx, &service.UsageBillingCommand{
		APIKeyID: 7, APIKeyQuotaCost: 2, BalanceCost: 2,
	}, &service.UsageBillingApplyResult{})
	require.ErrorIs(t, err, service.ErrAPIKeyNotFound)
	require.NoError(t, tx.Rollback())
	require.NoError(t, mock.ExpectationsWereMet())
}
