package enterpriseidentity

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestCreateEnterpriseRequiresActiveUpstreamUserAndSubscription(t *testing.T) {
	svc, mock := newMockService(t)
	anchor := time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT email, status FROM users WHERE id = \$1 FOR SHARE`).WithArgs(int64(99)).
		WillReturnRows(sqlmock.NewRows([]string{"email", "status"}).AddRow("owner@example.com", "active"))
	mock.ExpectQuery(`SELECT subscription\.id, subscription\.weekly_window_start`).WithArgs(int64(99)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "weekly_window_start"}).AddRow(int64(5), anchor))
	mock.ExpectQuery(`INSERT INTO enterprises`).WithArgs("Acme", int64(99), "acme.example.com").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "host", "dedicated", "status", "created_at"}).AddRow(int64(7), "Acme", "acme.example.com", int64(99), "active", time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)))
	mock.ExpectExec(`INSERT INTO enterprise_audit_events`).WithArgs(int64(7), sqlmock.AnyArg(), "platform_user:1").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery(`INSERT INTO enterprise_subscriptions`).WithArgs(int64(7), int64(5), sqlmock.AnyArg(), "platform_user:1").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(11)))
	mock.ExpectExec(`INSERT INTO enterprise_audit_events`).WithArgs(int64(7), int64(11), sqlmock.AnyArg(), "platform_user:1").WillReturnResult(sqlmock.NewResult(2, 1))
	mock.ExpectCommit()

	item, err := svc.CreateEnterprise(context.Background(), CreateEnterpriseInput{Name: "Acme", Host: "acme.example.com", DedicatedUpstreamUser: 99, Reason: "new tenant"}, 1)

	require.NoError(t, err)
	require.Equal(t, "o***@example.com", item.AdminEmail)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCreateEnterpriseRejectsUnavailableUpstreamUserWithoutInsert(t *testing.T) {
	svc, mock := newMockService(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT email, status FROM users WHERE id = \$1 FOR SHARE`).WithArgs(int64(99)).WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()

	_, err := svc.CreateEnterprise(context.Background(), CreateEnterpriseInput{Name: "Acme", Host: "acme.example.com", DedicatedUpstreamUser: 99, Reason: "new tenant"}, 1)

	require.ErrorIs(t, err, errDedicatedUserUnavailable)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCreateEnterpriseRejectsMissingSubscriptionWithoutInsert(t *testing.T) {
	svc, mock := newMockService(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT email, status FROM users WHERE id = \$1 FOR SHARE`).WithArgs(int64(99)).
		WillReturnRows(sqlmock.NewRows([]string{"email", "status"}).AddRow("owner@example.com", "active"))
	mock.ExpectQuery(`SELECT subscription\.id, subscription\.weekly_window_start`).WithArgs(int64(99)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "weekly_window_start"}))
	mock.ExpectRollback()

	_, err := svc.CreateEnterprise(context.Background(), CreateEnterpriseInput{Name: "Acme", Host: "acme.example.com", DedicatedUpstreamUser: 99, Reason: "new tenant"}, 1)

	require.ErrorIs(t, err, errSubscriptionUnavailable)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCreateEnterpriseRollsBackActivationWhenSubscriptionInsertFails(t *testing.T) {
	svc, mock := newMockService(t)
	anchor := time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT email, status FROM users WHERE id = \$1 FOR SHARE`).WithArgs(int64(99)).
		WillReturnRows(sqlmock.NewRows([]string{"email", "status"}).AddRow("owner@example.com", "active"))
	mock.ExpectQuery(`SELECT subscription\.id, subscription\.weekly_window_start`).WithArgs(int64(99)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "weekly_window_start"}).AddRow(int64(5), anchor))
	mock.ExpectQuery(`INSERT INTO enterprises`).WithArgs("Acme", int64(99), "acme.example.com").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "host", "dedicated", "status", "created_at"}).AddRow(int64(7), "Acme", "acme.example.com", int64(99), "active", time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)))
	mock.ExpectExec(`INSERT INTO enterprise_audit_events`).WithArgs(int64(7), sqlmock.AnyArg(), "platform_user:1").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery(`INSERT INTO enterprise_subscriptions`).WithArgs(int64(7), int64(5), sqlmock.AnyArg(), "platform_user:1").
		WillReturnError(sql.ErrTxDone)
	mock.ExpectRollback()

	_, err := svc.CreateEnterprise(context.Background(), CreateEnterpriseInput{Name: "Acme", Host: "acme.example.com", DedicatedUpstreamUser: 99, Reason: "new tenant"}, 1)

	require.ErrorIs(t, err, sql.ErrTxDone)
	require.NoError(t, mock.ExpectationsWereMet())
}
