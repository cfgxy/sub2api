package enterpriseidentity

import (
	"context"
	"database/sql"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestRequestResetAuditsEquivalentResponseWithoutIdentityDisclosure(t *testing.T) {
	svc, mock := newMockService(t)
	ctx := WithAuditActor(context.Background(), "enterprise_public")
	mock.ExpectQuery("SELECT id, name, LOWER.*FROM enterprises").WithArgs("acme.example.com").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "host", "admin_user_id", "status"}).AddRow(1, "Acme", "acme.example.com", 5, "active"))
	mock.ExpectQuery("SELECT id, current_email FROM enterprise_employees").WithArgs(int64(1), "missing@example.com").WillReturnError(sql.ErrNoRows)
	mock.ExpectExec(`INSERT INTO enterprise_audit_events`).WithArgs(int64(1), "password.reset_requested", "employee", nil, sqlmock.AnyArg(), "enterprise_public").WillReturnResult(sqlmock.NewResult(1, 1))

	require.NoError(t, svc.RequestReset(ctx, "acme.example.com", "missing@example.com", "https://acme.example.com/reset", "zh-CN"))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestResetPasswordAuditsInvalidTokenWithoutPersistingToken(t *testing.T) {
	svc, mock := newMockService(t)
	ctx := WithAuditActor(context.Background(), "enterprise_public")
	mock.ExpectQuery("SELECT id, name, LOWER.*FROM enterprises").WithArgs("acme.example.com").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "host", "admin_user_id", "status"}).AddRow(1, "Acme", "acme.example.com", 5, "active"))
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT reset.id, reset.employee_id.*enterprise_password_reset_tokens").WithArgs(int64(1), tokenHash("bad-token")).WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()
	mock.ExpectExec(`INSERT INTO enterprise_audit_events`).WithArgs(int64(1), "password.reset", "employee", nil, sqlmock.AnyArg(), "enterprise_public").WillReturnResult(sqlmock.NewResult(1, 1))

	require.ErrorIs(t, svc.ResetPassword(ctx, "acme.example.com", "bad-token", "new-strong-password"), errResetInvalid)
	require.NoError(t, mock.ExpectationsWereMet())
}
