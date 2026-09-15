package enterpriseidentity

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestRevokeSessionWritesRedactedEnterpriseAudit(t *testing.T) {
	svc, mock := newMockService(t)
	claims := &Claims{EnterpriseID: 7, PrincipalType: "employee", PrincipalID: 9, SessionID: "session-current"}
	ctx := WithAuditActor(context.Background(), "enterprise_employee:9")
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE enterprise_sessions SET revoked_at`).WithArgs(int64(7), "employee", int64(9), "session-target").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO enterprise_audit_events`).WithArgs(int64(7), "session.revoke", "session", nil, sqlmock.AnyArg(), "enterprise_employee:9").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	require.NoError(t, svc.RevokeSession(ctx, claims, "session-target"))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRevokeAllSessionsWritesRedactedEnterpriseAudit(t *testing.T) {
	svc, mock := newMockService(t)
	claims := &Claims{EnterpriseID: 7, PrincipalType: "admin", PrincipalID: 9}
	ctx := WithAuditActor(context.Background(), "enterprise_admin:9")
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE enterprise_sessions SET revoked_at`).WithArgs(int64(7), "admin", int64(9)).WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec(`INSERT INTO enterprise_audit_events`).WithArgs(int64(7), "session.revoke_all", "session", nil, sqlmock.AnyArg(), "enterprise_admin:9").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	require.NoError(t, svc.RevokeAllSessions(ctx, claims))
	require.NoError(t, mock.ExpectationsWereMet())
}
