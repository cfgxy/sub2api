package enterpriseidentity

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	jwt "github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

// SHAN-388 P1：企业域密码统一「最少 8 个字符，无组成复杂度要求」。
// 以下测试同时固定 7 位拒绝与 8 位放行的边界，防止口径再分裂。

func TestChangeInitialPasswordRejectsSevenCharacters(t *testing.T) {
	svc, mock := newMockService(t)
	claims := &Claims{EnterpriseID: 1, PrincipalType: "employee", PrincipalID: 2, SessionID: "sess-1"}

	err := svc.ChangeInitialPassword(WithAuditActor(context.Background(), "enterprise_test"), claims, "Ab1cd12")

	require.ErrorContains(t, err, "WEAK_PASSWORD")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestChangeInitialPasswordAcceptsEightCharactersWithoutCurrentPassword(t *testing.T) {
	svc, mock := newMockService(t)
	ctx := WithAuditActor(context.Background(), "enterprise_test")
	claims := &Claims{EnterpriseID: 1, PrincipalType: "employee", PrincipalID: 2, SessionID: "sess-current"}

	mock.ExpectBegin()
	// 首改不再要求当前初始密码：更新语句不得携带 auth_version 自增（否则当前 token 立即失效）。
	mock.ExpectExec(`^UPDATE enterprise_employees\s+SET password_hash = \$1, must_change_password = FALSE, initial_password_expires_at = NULL,\s+password_changed_at = NOW\(\), updated_at = NOW\(\)\s+WHERE enterprise_id = \$2 AND id = \$3 AND status = 'active'$`).
		WithArgs(sqlmock.AnyArg(), int64(1), int64(2)).
		WillReturnResult(sqlmock.NewResult(1, 1))
	// 保留当前会话：仅撤销该员工的其他会话（id <> 当前会话）。
	mock.ExpectExec(`^UPDATE enterprise_sessions SET revoked_at = NOW\(\), updated_at = NOW\(\)\s+WHERE enterprise_id = \$1 AND principal_type = 'employee' AND principal_id = \$2 AND revoked_at IS NULL AND id <> \$3$`).
		WithArgs(int64(1), int64(2), "sess-current").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`INSERT INTO enterprise_audit_events`).
		WithArgs(int64(1), "employee.password_changed", "employee", int64(2), sqlmock.AnyArg(), "enterprise_test").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	require.NoError(t, svc.ChangeInitialPassword(ctx, claims, "Ab1cd123"))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestChangeInitialPasswordStillRejectsAdministratorPrincipal(t *testing.T) {
	svc, mock := newMockService(t)

	err := svc.ChangeInitialPassword(context.Background(), &Claims{PrincipalType: "admin"}, "Ab1cd123")

	require.ErrorContains(t, err, "EMPLOYEE_PASSWORD_ONLY")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestChangePasswordAcceptsEightCharactersAndKeepsRevokingAllSessions(t *testing.T) {
	svc, mock := newMockService(t)
	ctx := WithAuditActor(context.Background(), "enterprise_test")
	claims := &Claims{EnterpriseID: 1, PrincipalType: "employee", PrincipalID: 2, SessionID: "sess-1"}

	require.ErrorContains(t, svc.ChangePassword(ctx, claims, "current-pass", "Ab1cd12"), "WEAK_PASSWORD")
	require.NoError(t, mock.ExpectationsWereMet())

	currentHash, err := bcrypt.GenerateFromPassword([]byte("current-pass"), bcrypt.MinCost)
	require.NoError(t, err)
	mock.ExpectQuery(`^SELECT password_hash FROM enterprise_employees WHERE enterprise_id = \$1 AND id = \$2 AND status = 'active'$`).
		WithArgs(int64(1), int64(2)).
		WillReturnRows(sqlmock.NewRows([]string{"password_hash"}).AddRow(string(currentHash)))
	mock.ExpectBegin()
	mock.ExpectExec(`^UPDATE enterprise_employees SET password_hash = \$1, must_change_password = FALSE, initial_password_expires_at = NULL, password_changed_at = NOW\(\), auth_version = auth_version \+ 1, updated_at = NOW\(\) WHERE enterprise_id = \$2 AND id = \$3 AND status = 'active'$`).
		WithArgs(sqlmock.AnyArg(), int64(1), int64(2)).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(`^UPDATE enterprise_sessions SET revoked_at = NOW\(\), updated_at = NOW\(\) WHERE enterprise_id = \$1 AND principal_type = 'employee' AND principal_id = \$2 AND revoked_at IS NULL$`).
		WithArgs(int64(1), int64(2)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`INSERT INTO enterprise_audit_events`).
		WithArgs(int64(1), "employee.password_changed", "employee", int64(2), sqlmock.AnyArg(), "enterprise_test").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	require.NoError(t, svc.ChangePassword(ctx, claims, "current-pass", "Ab1cd123"))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestResetPasswordAcceptsEightCharacters(t *testing.T) {
	svc, mock := newMockService(t)
	ctx := WithAuditActor(context.Background(), "enterprise_test")

	mock.ExpectQuery(`SELECT id, name, LOWER\(BTRIM\(portal_host\)\), admin_user_id, status`).
		WithArgs("acme.example.com").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "host", "admin_user_id", "status"}).AddRow(1, "Acme", "acme.example.com", 5, "active"))
	mock.ExpectExec(`INSERT INTO enterprise_audit_events`).
		WithArgs(int64(1), "password.reset", "employee", nil, sqlmock.AnyArg(), "enterprise_test").
		WillReturnResult(sqlmock.NewResult(1, 1))
	require.ErrorContains(t, svc.ResetPassword(ctx, "acme.example.com", "one-time-token", "Ab1cd12"), "WEAK_PASSWORD")
	require.NoError(t, mock.ExpectationsWereMet())

	mock.ExpectQuery(`SELECT id, name, LOWER\(BTRIM\(portal_host\)\), admin_user_id, status`).
		WithArgs("acme.example.com").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "host", "admin_user_id", "status"}).AddRow(1, "Acme", "acme.example.com", 5, "active"))
	mock.ExpectBegin()
	mock.ExpectQuery(`^SELECT reset\.id, reset\.employee_id`).
		WithArgs(int64(1), tokenHash("one-time-token")).
		WillReturnRows(sqlmock.NewRows([]string{"id", "employee_id"}).AddRow("reset-1", int64(2)))
	mock.ExpectExec(`^UPDATE enterprise_password_reset_tokens SET used_at = NOW\(\)`).
		WithArgs(int64(1), "reset-1").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(`^UPDATE enterprise_employees`).
		WithArgs(sqlmock.AnyArg(), int64(1), int64(2)).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(`^UPDATE enterprise_sessions`).
		WithArgs(int64(1), int64(2)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`INSERT INTO enterprise_audit_events`).
		WithArgs(int64(1), "password.reset", "employee", int64(2), sqlmock.AnyArg(), "enterprise_test").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	require.NoError(t, svc.ResetPassword(ctx, "acme.example.com", "one-time-token", "Ab1cd123"))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCreateEmployeeRejectsSevenCharacterInitialPassword(t *testing.T) {
	svc, mock := newMockService(t)
	ctx := WithAuditActor(context.Background(), "enterprise_test")

	_, err := svc.CreateEmployee(ctx, 1, "emp@example.com", "Ab1cd12", nil)

	require.ErrorContains(t, err, "INVALID_EMPLOYEE")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCreateEmployeeAcceptsEightCharacterInitialPassword(t *testing.T) {
	svc, mock := newMockService(t)
	ctx := WithAuditActor(context.Background(), "enterprise_test")

	mock.ExpectBegin()
	mock.ExpectQuery(`^INSERT INTO enterprise_employees`).
		WithArgs(int64(1), "emp@example.com", sqlmock.AnyArg(), nil).
		WillReturnRows(sqlmock.NewRows([]string{"id", "current_email", "status", "department_id", "must_change_password", "version"}).
			AddRow(int64(9), "emp@example.com", "active", nil, true, int64(1)))
	mock.ExpectExec(`INSERT INTO enterprise_audit_events`).
		WithArgs(int64(1), "employee.created", "employee", int64(9), sqlmock.AnyArg(), "enterprise_test").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	item, err := svc.CreateEmployee(ctx, 1, "emp@example.com", "Ab1cd123", nil)
	require.NoError(t, err)
	require.NotNil(t, item)
	require.NoError(t, mock.ExpectationsWereMet())
}

// SHAN-388 N1：管理员重置员工密码，生成可交付的新初始密码并强制首改。

func TestResetEmployeePasswordIssuesDeliverableInitialPassword(t *testing.T) {
	svc, mock := newMockService(t)
	ctx := WithAuditActor(context.Background(), "enterprise_test")
	admin := &Claims{EnterpriseID: 1, PrincipalType: "admin", PrincipalID: 5}

	mock.ExpectBegin()
	mock.ExpectExec(`^UPDATE enterprise_employees SET password_hash = \$1, must_change_password = TRUE, initial_password_expires_at = NOW\(\) \+ INTERVAL '24 hours',\s+auth_version = auth_version \+ 1, updated_at = NOW\(\)\s+WHERE enterprise_id = \$2 AND id = \$3 AND status <> 'terminated'$`).
		WithArgs(sqlmock.AnyArg(), int64(1), int64(2)).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(`^UPDATE enterprise_sessions SET revoked_at = NOW\(\), updated_at = NOW\(\)\s+WHERE enterprise_id = \$1 AND principal_type = 'employee' AND principal_id = \$2 AND revoked_at IS NULL$`).
		WithArgs(int64(1), int64(2)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`INSERT INTO enterprise_audit_events`).
		WithArgs(int64(1), "employee.password_reset_by_admin", "employee", int64(2), sqlmock.AnyArg(), "enterprise_test").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	password, err := svc.ResetEmployeePassword(ctx, admin, 2)
	require.NoError(t, err)
	require.Len(t, password, initialPasswordLength)
	require.Regexp(t, `^[A-HJ-NP-Za-km-z2-9]+$`, password)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestResetEmployeePasswordRejectsNonAdminPrincipal(t *testing.T) {
	svc, mock := newMockService(t)

	_, err := svc.ResetEmployeePassword(context.Background(), &Claims{EnterpriseID: 1, PrincipalType: "employee", PrincipalID: 2}, 3)

	require.Error(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRegisterRoutesIncludesAdminEmployeePasswordReset(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	h := NewHandler(nil, nil, nil, nil)
	h.RegisterRoutes(router.Group("/api/v1"))

	routes := make(map[string]bool)
	for _, route := range router.Routes() {
		routes[route.Method+" "+route.Path] = true
	}
	require.True(t, routes["POST /api/v1/enterprise/admin/employees/:id/reset-password"])
}

// SHAN-388 P2：首改拦截判定以数据库现值为准，不用 JWT 内过期 claim，
// 使「首改成功后携原 token 直进首页」成立。

func TestAuthenticateMiddlewareUsesDatabaseForceChangeTruth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		name          string
		dbForceChange bool
		method        string
		path          string
		wantAllowed   bool
	}{
		{name: "cleared db flag passes with stale jwt claim", dbForceChange: false, method: http.MethodGet, path: "/api/v1/enterprise/profile", wantAllowed: true},
		{name: "db flag still set blocks regular routes", dbForceChange: true, method: http.MethodGet, path: "/api/v1/enterprise/profile", wantAllowed: false},
		{name: "db flag allows first-change route", dbForceChange: true, method: http.MethodPost, path: "/api/v1/enterprise/password/first-change", wantAllowed: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, mock := newMockService(t)
			claims := &Claims{
				EnterpriseID: 1, PrincipalType: "employee", PrincipalID: 2,
				AuthVersion: 3, SessionID: "sess-1", ForceChange: true,
			}
			raw, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(svc.secret)
			require.NoError(t, err)

			mock.ExpectQuery(`SELECT id, name, LOWER\(BTRIM\(portal_host\)\), admin_user_id, status`).
				WithArgs("acme.example.com").
				WillReturnRows(sqlmock.NewRows([]string{"id", "name", "host", "admin_user_id", "status"}).AddRow(1, "Acme", "acme.example.com", 5, "active"))
			mock.ExpectQuery(`SELECT current_email, status, auth_version, must_change_password`).
				WithArgs(int64(1), int64(2)).
				WillReturnRows(sqlmock.NewRows([]string{"current_email", "status", "auth_version", "must_change_password"}).
					AddRow("emp@example.com", "active", int64(3), tc.dbForceChange))
			mock.ExpectQuery(`SELECT EXISTS`).
				WithArgs(int64(1), "sess-1").
				WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))

			router := gin.New()
			h := NewHandler(svc, nil, nil, nil)
			router.Use(h.authenticate())
			handler := func(c *gin.Context) { c.String(http.StatusOK, "ok") }
			router.GET("/api/v1/enterprise/profile", handler)
			router.POST("/api/v1/enterprise/password/first-change", handler)

			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.Host = "acme.example.com"
			req.Header.Set("Authorization", "Bearer "+raw)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if tc.wantAllowed {
				require.Equal(t, http.StatusOK, rec.Code)
			} else {
				require.Equal(t, http.StatusForbidden, rec.Code)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
