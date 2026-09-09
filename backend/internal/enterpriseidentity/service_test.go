package enterpriseidentity

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

type failingResetMailer struct{ err error }

func (m failingResetMailer) SendEmail(context.Context, string, string, string) error { return m.err }

func expectedUserTokenVersion(email, passwordHash string) int64 {
	material := strings.ToLower(strings.TrimSpace(email)) + "\n" + passwordHash
	sum := sha256.Sum256([]byte(material))
	return int64(binary.BigEndian.Uint64(sum[:8]) & 0x7fffffffffffffff)
}

func newMockService(t *testing.T) (*Service, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return NewService(db, &config.Config{JWT: config.JWTConfig{Secret: "0123456789abcdef0123456789abcdef"}}, nil), mock
}

func TestCreateEmployeeScopesSameEmailByEnterprise(t *testing.T) {
	svc, mock := newMockService(t)
	query := regexp.QuoteMeta("INSERT INTO enterprise_employees") + ".*"
	mock.ExpectQuery(query).WithArgs(int64(1), "same@example.com", sqlmock.AnyArg(), nil).
		WillReturnRows(sqlmock.NewRows([]string{"id", "current_email", "status", "department_id", "must_change_password"}).
			AddRow(10, "same@example.com", "active", nil, true))
	mock.ExpectQuery(query).WithArgs(int64(2), "same@example.com", sqlmock.AnyArg(), nil).
		WillReturnRows(sqlmock.NewRows([]string{"id", "current_email", "status", "department_id", "must_change_password"}).
			AddRow(20, "same@example.com", "active", nil, true))

	one, err := svc.CreateEmployee(context.Background(), 1, "same@example.com", "strong-password-1", nil)
	require.NoError(t, err)
	two, err := svc.CreateEmployee(context.Background(), 2, "same@example.com", "strong-password-2", nil)
	require.NoError(t, err)
	require.NotEqual(t, one.ID, two.ID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestResetPasswordBindsEnterpriseAndRejectsReplay(t *testing.T) {
	svc, mock := newMockService(t)
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }

	expectEnterprise := func() {
		mock.ExpectQuery("SELECT id, name, LOWER.*FROM enterprises").WithArgs("acme.example.com").
			WillReturnRows(sqlmock.NewRows([]string{"id", "name", "host", "admin_user_id", "status"}).AddRow(1, "Acme", "acme.example.com", 5, "active"))
	}
	expectEnterprise()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT reset.id, reset.employee_id.*enterprise_password_reset_tokens").WithArgs(int64(1), tokenHash("one-time-token")).
		WillReturnRows(sqlmock.NewRows([]string{"id", "employee_id"}).AddRow("11111111-1111-1111-1111-111111111111", 9))
	mock.ExpectExec("UPDATE enterprise_password_reset_tokens SET used_at").WithArgs(int64(1), "11111111-1111-1111-1111-111111111111").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE enterprise_employees").WithArgs(sqlmock.AnyArg(), int64(1), int64(9)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE enterprise_sessions SET revoked_at").WithArgs(int64(1), int64(9)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	require.NoError(t, svc.ResetPassword(context.Background(), "acme.example.com", "one-time-token", "new-strong-password"))

	expectEnterprise()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT reset.id, reset.employee_id.*enterprise_password_reset_tokens").WithArgs(int64(1), tokenHash("one-time-token")).WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()
	require.ErrorIs(t, svc.ResetPassword(context.Background(), "acme.example.com", "one-time-token", "new-strong-password"), errResetInvalid)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAuthenticateRejectsDisabledEmployeeImmediately(t *testing.T) {
	svc, mock := newMockService(t)
	now := time.Now()
	svc.now = func() time.Time { return now }
	claims := Claims{EnterpriseID: 1, PrincipalType: "employee", PrincipalID: 9, AuthVersion: 3, Role: "enterprise_employee", SessionID: "11111111-1111-1111-1111-111111111111", RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour))}}
	raw, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(svc.secret)
	require.NoError(t, err)
	mock.ExpectQuery("SELECT id, name, LOWER.*FROM enterprises").WithArgs("acme.example.com").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "host", "admin_user_id", "status"}).AddRow(1, "Acme", "acme.example.com", 5, "active"))
	mock.ExpectQuery("SELECT current_email, status, auth_version, must_change_password").WithArgs(int64(1), int64(9)).
		WillReturnRows(sqlmock.NewRows([]string{"current_email", "status", "auth_version", "must_change_password"}).AddRow("employee@example.com", "disabled", 3, false))

	_, _, err = svc.Authenticate(context.Background(), "acme.example.com", raw)
	require.ErrorIs(t, err, errInactive)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAuthenticateRequiresHostAndTokenEnterpriseToMatch(t *testing.T) {
	svc, mock := newMockService(t)
	now := time.Now()
	claims := Claims{EnterpriseID: 2, PrincipalType: "employee", PrincipalID: 9, AuthVersion: 3, Role: "enterprise_employee", SessionID: "11111111-1111-1111-1111-111111111111", RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour))}}
	raw, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(svc.secret)
	require.NoError(t, err)
	mock.ExpectQuery("SELECT id, name, LOWER.*FROM enterprises").WithArgs("acme.example.com").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "host", "admin_user_id", "status"}).AddRow(1, "Acme", "acme.example.com", 5, "active"))

	_, _, err = svc.Authenticate(context.Background(), "acme.example.com", raw)
	require.ErrorIs(t, err, errWrongHost)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAuthenticateRejectsDisabledEnterpriseImmediately(t *testing.T) {
	svc, mock := newMockService(t)
	now := time.Now()
	claims := Claims{EnterpriseID: 1, PrincipalType: "employee", PrincipalID: 9, AuthVersion: 3, Role: "enterprise_employee", SessionID: "11111111-1111-1111-1111-111111111111", RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour))}}
	raw, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(svc.secret)
	require.NoError(t, err)
	mock.ExpectQuery("SELECT id, name, LOWER.*FROM enterprises").WithArgs("acme.example.com").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "host", "admin_user_id", "status"}).AddRow(1, "Acme", "acme.example.com", 5, "disabled"))

	_, _, err = svc.Authenticate(context.Background(), "acme.example.com", raw)
	require.ErrorIs(t, err, errInactive)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAdminPasswordChangeInvalidatesEnterpriseAccessToken(t *testing.T) {
	svc, mock := newMockService(t)
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	oldHash, err := bcrypt.GenerateFromPassword([]byte("old-password-strong"), bcrypt.MinCost)
	require.NoError(t, err)
	newHash, err := bcrypt.GenerateFromPassword([]byte("new-password-strong"), bcrypt.MinCost)
	require.NoError(t, err)
	version := expectedUserTokenVersion("admin@example.com", string(oldHash))

	mock.ExpectQuery("SELECT id, name, LOWER.*FROM enterprises").WithArgs("acme.example.com").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "host", "admin_user_id", "status"}).AddRow(1, "Acme", "acme.example.com", 5, "active"))
	mock.ExpectQuery("SELECT 'admin'.*users.password_hash").WithArgs(int64(5), "admin@example.com", int64(1)).
		WillReturnRows(sqlmock.NewRows([]string{"principal_type", "id", "email", "role", "password_hash", "status", "auth_version", "force_change", "expires"}).
			AddRow("admin", 5, "admin@example.com", "enterprise_admin", string(oldHash), "active", version, false, nil))
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO enterprise_sessions").WithArgs(sqlmock.AnyArg(), int64(1), "admin", int64(5), sqlmock.AnyArg(), version, sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO enterprise_refresh_tokens").WithArgs(sqlmock.AnyArg(), int64(1), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	pair, err := svc.Login(context.Background(), "acme.example.com", "admin@example.com", "old-password-strong", "ua", "127.0.0.1")
	require.NoError(t, err)

	mock.ExpectQuery("SELECT id, name, LOWER.*FROM enterprises").WithArgs("acme.example.com").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "host", "admin_user_id", "status"}).AddRow(1, "Acme", "acme.example.com", 5, "active"))
	mock.ExpectQuery("SELECT users.email, users.password_hash, users.status").WithArgs(int64(1), int64(5)).
		WillReturnRows(sqlmock.NewRows([]string{"email", "password_hash", "status"}).AddRow("admin@example.com", string(newHash), "active"))

	_, _, err = svc.Authenticate(context.Background(), "acme.example.com", pair.AccessToken)
	require.ErrorIs(t, err, errInvalidToken)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRefreshReplayRevokesEntireFamily(t *testing.T) {
	svc, mock := newMockService(t)
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	const (
		r0        = "refresh-r0"
		sessionID = "11111111-1111-1111-1111-111111111111"
		familyID  = "22222222-2222-2222-2222-222222222222"
	)
	expires := now.Add(refreshTokenTTL)
	expectEnterprise := func() {
		mock.ExpectQuery("SELECT id, name, LOWER.*FROM enterprises").WithArgs("acme.example.com").
			WillReturnRows(sqlmock.NewRows([]string{"id", "name", "host", "admin_user_id", "status"}).AddRow(1, "Acme", "acme.example.com", 5, "active"))
	}

	expectEnterprise()
	mock.ExpectBegin()
	mock.ExpectQuery("FROM enterprise_refresh_tokens AS token.*JOIN enterprise_sessions AS session").WithArgs(int64(1), tokenHash(r0)).
		WillReturnRows(sqlmock.NewRows([]string{"session_id", "principal_type", "principal_id", "family_id", "auth_version", "token_expires", "consumed_at", "token_revoked_at", "session_expires", "session_revoked_at"}).
			AddRow(sessionID, "employee", 9, familyID, 3, expires, nil, nil, expires, nil))
	mock.ExpectQuery("SELECT current_email, status, auth_version, must_change_password").WithArgs(int64(1), int64(9)).
		WillReturnRows(sqlmock.NewRows([]string{"current_email", "status", "auth_version", "must_change_password"}).AddRow("employee@example.com", "active", 3, false))
	mock.ExpectExec("UPDATE enterprise_refresh_tokens.*SET consumed_at").WithArgs(int64(1), tokenHash(r0)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE enterprise_sessions SET user_agent").WithArgs("ua", "127.0.0.1", int64(1), sessionID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO enterprise_refresh_tokens").WithArgs(sqlmock.AnyArg(), int64(1), sessionID, familyID, sqlmock.AnyArg(), expires).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	r1, err := svc.Refresh(context.Background(), "acme.example.com", r0, "ua", "127.0.0.1")
	require.NoError(t, err)
	require.NotEmpty(t, r1.RefreshToken)
	require.NotEqual(t, r0, r1.RefreshToken)

	expectEnterprise()
	mock.ExpectBegin()
	mock.ExpectQuery("FROM enterprise_refresh_tokens AS token.*JOIN enterprise_sessions AS session").WithArgs(int64(1), tokenHash(r0)).
		WillReturnRows(sqlmock.NewRows([]string{"session_id", "principal_type", "principal_id", "family_id", "auth_version", "token_expires", "consumed_at", "token_revoked_at", "session_expires", "session_revoked_at"}).
			AddRow(sessionID, "employee", 9, familyID, 3, expires, now, nil, expires, nil))
	mock.ExpectExec("UPDATE enterprise_sessions.*refresh_family_id").WithArgs(int64(1), familyID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE enterprise_refresh_tokens.*refresh_family_id").WithArgs(int64(1), familyID).WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectCommit()
	_, err = svc.Refresh(context.Background(), "acme.example.com", r0, "ua", "127.0.0.1")
	require.ErrorIs(t, err, errInvalidToken)

	expectEnterprise()
	mock.ExpectBegin()
	mock.ExpectQuery("FROM enterprise_refresh_tokens AS token.*JOIN enterprise_sessions AS session").WithArgs(int64(1), tokenHash(r1.RefreshToken)).
		WillReturnRows(sqlmock.NewRows([]string{"session_id", "principal_type", "principal_id", "family_id", "auth_version", "token_expires", "consumed_at", "token_revoked_at", "session_expires", "session_revoked_at"}).
			AddRow(sessionID, "employee", 9, familyID, 3, expires, nil, now, expires, now))
	mock.ExpectRollback()
	_, err = svc.Refresh(context.Background(), "acme.example.com", r1.RefreshToken, "ua", "127.0.0.1")
	require.ErrorIs(t, err, errInvalidToken)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestLogoutRejectsUnknownRefreshToken(t *testing.T) {
	svc, mock := newMockService(t)
	mock.ExpectQuery("SELECT id, name, LOWER.*FROM enterprises").WithArgs("acme.example.com").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "host", "admin_user_id", "status"}).AddRow(1, "Acme", "acme.example.com", 5, "active"))
	mock.ExpectBegin()
	mock.ExpectQuery("FROM enterprise_refresh_tokens AS token.*JOIN enterprise_sessions AS session").WithArgs(int64(1), tokenHash("missing")).WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()
	require.ErrorIs(t, svc.Logout(context.Background(), "acme.example.com", "missing"), errInvalidToken)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRequestResetReturnsSuccessAndDeletesTokenWhenDeliveryFails(t *testing.T) {
	svc, mock := newMockService(t)
	svc.mailer = failingResetMailer{err: errors.New("smtp unavailable")}
	mock.ExpectQuery("SELECT id, name, LOWER.*FROM enterprises").WithArgs("acme.example.com").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "host", "admin_user_id", "status"}).AddRow(1, "Acme", "acme.example.com", 5, "active"))
	mock.ExpectQuery("SELECT id, current_email FROM enterprise_employees").WithArgs(int64(1), "employee@example.com").
		WillReturnRows(sqlmock.NewRows([]string{"id", "current_email"}).AddRow(9, "employee@example.com"))
	mock.ExpectExec("INSERT INTO enterprise_password_reset_tokens").WithArgs(sqlmock.AnyArg(), int64(1), int64(9), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("DELETE FROM enterprise_password_reset_tokens").WithArgs(int64(1), int64(9), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, svc.RequestReset(context.Background(), "acme.example.com", "employee@example.com", "https://acme.example.com/reset", "en"))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRequestResetReturnsSuccessForUnavailableIdentity(t *testing.T) {
	svc, mock := newMockService(t)
	mock.ExpectQuery("SELECT id, name, LOWER.*FROM enterprises").WithArgs("acme.example.com").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "host", "admin_user_id", "status"}).AddRow(1, "Acme", "acme.example.com", 5, "active"))
	mock.ExpectQuery("SELECT id, current_email FROM enterprise_employees").WithArgs(int64(1), "inactive@example.com").WillReturnError(sql.ErrNoRows)
	require.NoError(t, svc.RequestReset(context.Background(), "acme.example.com", "inactive@example.com", "https://acme.example.com/reset", "en"))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRequestResetDeletesTokenWhenMailerIsUnavailable(t *testing.T) {
	svc, mock := newMockService(t)
	mock.ExpectQuery("SELECT id, name, LOWER.*FROM enterprises").WithArgs("acme.example.com").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "host", "admin_user_id", "status"}).AddRow(1, "Acme", "acme.example.com", 5, "active"))
	mock.ExpectQuery("SELECT id, current_email FROM enterprise_employees").WithArgs(int64(1), "employee@example.com").
		WillReturnRows(sqlmock.NewRows([]string{"id", "current_email"}).AddRow(9, "employee@example.com"))
	mock.ExpectExec("INSERT INTO enterprise_password_reset_tokens").WithArgs(sqlmock.AnyArg(), int64(1), int64(9), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("DELETE FROM enterprise_password_reset_tokens").WithArgs(int64(1), int64(9), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, svc.RequestReset(context.Background(), "acme.example.com", "employee@example.com", "https://acme.example.com/reset", "en"))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestTerminateEmployeeReleasesEmailForNewEmployeeID(t *testing.T) {
	svc, mock := newMockService(t)
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE enterprise_employees SET status = 'terminated'").WithArgs(int64(1), int64(10)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE enterprise_sessions SET revoked_at").WithArgs(int64(1), int64(10)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	require.NoError(t, svc.TerminateEmployee(context.Background(), 1, 10))

	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO enterprise_employees")+".*").WithArgs(int64(1), "rehire@example.com", sqlmock.AnyArg(), nil).
		WillReturnRows(sqlmock.NewRows([]string{"id", "current_email", "status", "department_id", "must_change_password"}).AddRow(11, "rehire@example.com", "active", nil, true))
	employee, err := svc.CreateEmployee(context.Background(), 1, "rehire@example.com", "strong-password", nil)
	require.NoError(t, err)
	require.Equal(t, int64(11), employee.ID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDeleteDepartmentClearsEmployeesBeforeDisabling(t *testing.T) {
	svc, mock := newMockService(t)
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE enterprise_employees SET department_id = NULL").WithArgs(int64(1), int64(7)).WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec("UPDATE enterprise_departments SET status = 'disabled'").WithArgs(int64(1), int64(7)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	require.NoError(t, svc.DeleteDepartment(context.Background(), 1, 7))
	require.NoError(t, mock.ExpectationsWereMet())
}
