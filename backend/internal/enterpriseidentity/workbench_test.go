package enterpriseidentity

import (
	"context"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestParseWorkbenchQueryValidatesScopeAndPagination(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/", func(c *gin.Context) {
		query, ok := parseWorkbenchQuery(c, true)
		require.True(t, ok)
		require.Equal(t, int64(17), *query.EmployeeID)
		require.Equal(t, "week", query.WindowType)
		require.Equal(t, 2, query.Page)
		require.Equal(t, 50, query.PageSize)
		require.Equal(t, time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC), *query.WindowAnchor)
	})
	req := httptest.NewRequest("GET", "/?employee_id=17&window_type=week&window_anchor=2026-09-08T00:00:00Z&page=2&page_size=50", nil)
	router.ServeHTTP(httptest.NewRecorder(), req)
}

func TestParseWorkbenchQueryRejectsInvalidWindowAndRange(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, target := range []string{
		"/?window_type=quarter",
		"/?window_anchor=2026-09-08T00:00:00Z",
		"/?start_at=2026-09-09T00:00:00Z&end_at=2026-09-08T00:00:00Z",
		"/?page=0",
	} {
		router := gin.New()
		router.GET("/", func(c *gin.Context) {
			_, ok := parseWorkbenchQuery(c, true)
			require.False(t, ok, target)
		})
		router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", target, nil))
	}
}

func TestSanitizeAuditPayloadRemovesSensitiveFields(t *testing.T) {
	payload := sanitizeAuditPayload([]byte(`{"api_key_id":7,"masked_key":"sk-abc...1234","password":"hidden","session_id":"hidden","nested":{"token":"hidden"}}`))
	require.Equal(t, float64(7), payload["api_key_id"])
	require.Equal(t, "sk-abc...1234", payload["masked_key"])
	require.NotContains(t, payload, "password")
	require.NotContains(t, payload, "session_id")
}

func TestGetSummaryUsesCanonicalWindowAndScansDecimalCredit(t *testing.T) {
	service, mock := newMockService(t)
	handler := &WorkbenchHandler{owner: &Handler{service: service}}

	mock.ExpectQuery(`SELECT COALESCE\(SUM\(COALESCE\(usage_log\.actual_cost, 0\)\), 0\)::text, COUNT\(\*\)`).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"total_usage_credit", "total_requests"}).AddRow("2.75", int64(3)))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM enterprise_employees`).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(1)))
	mock.ExpectQuery(`SELECT COUNT\(DISTINCT attribution\.employee_id\)`).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(1)))
	mock.ExpectQuery(`(?s)WITH employee_window_usage AS .*attribution\.window_type = 'week'.*allocation\.subscription_id = usage\.subscription_id.*allocation\.window_type = usage\.window_type.*allocation\.window_anchor = usage\.window_anchor`).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"employee_id", "email", "department_id", "requests", "configured_credit", "usage_credit"}).
			AddRow(int64(22), "employee@example.com", int64(3), int64(3), "12.50", "2.75"))

	result, err := handler.getSummary(t.Context(), 7, workbenchQuery{})
	require.NoError(t, err)
	require.Equal(t, "2.75", result.TotalUsageCredit)
	require.Equal(t, int64(3), result.TotalRequests)
	require.Len(t, result.EmployeeSummaries, 1)
	require.Equal(t, int64(3), result.EmployeeSummaries[0].Requests)
	require.Equal(t, "12.50", result.EmployeeSummaries[0].ConfiguredCredit)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSanitizeAuditActorRefRemovesLegacySessionIdentifiers(t *testing.T) {
	require.Equal(t, "enterprise_actor", sanitizeAuditActorRef("enterprise_session:legacy-session-id"))
	require.Equal(t, "enterprise_employee:22", sanitizeAuditActorRef("enterprise_employee:22"))
}

// TestListAuditEventsSurvivesEmployeeTerminationAndScopesByEnterprise proves
// two of SHAN-239's acceptance criteria against the real query: (1) audit
// history for an employee remains queryable after termination, because the
// query only joins on payload->>'employee_id' and never filters on the
// employee's current status; (2) every variant is always scoped by
// enterprise_id first, so one enterprise's admin can never see another
// enterprise's audit trail even when supplying the same employee_id.
func TestListAuditEventsSurvivesEmployeeTerminationAndScopesByEnterprise(t *testing.T) {
	service, mock := newMockService(t)
	handler := &WorkbenchHandler{owner: &Handler{service: service}}
	employeeID := int64(10)
	terminatedAt := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) FROM enterprise_audit_events WHERE enterprise_id = $1 AND payload->>'employee_id' = $2::text")).
		WithArgs(int64(1), employeeID).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(1)))
	mock.ExpectQuery(regexp.QuoteMeta("WHERE enterprise_id = $1 AND payload->>'employee_id' = $2::text\n\t\tORDER BY created_at DESC, id DESC")).
		WithArgs(int64(1), employeeID, 20, 0).
		WillReturnRows(sqlmock.NewRows([]string{"id", "event_type", "entity_type", "entity_id", "payload", "actor_ref", "created_at"}).
			AddRow(int64(99), "employee.terminated", "enterprise_employee", employeeID, []byte(`{"employee_id":10}`), "admin:42", terminatedAt))

	items, total, err := handler.listAuditEvents(context.Background(), 1, workbenchQuery{EmployeeID: &employeeID, Page: 1, PageSize: 20}, "", "")
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, items, 1)
	require.Equal(t, "employee.terminated", items[0].EventType)
	require.NoError(t, mock.ExpectationsWereMet())

	// The second enterprise's admin queries the same employee_id and must
	// see nothing: the enterprise_id predicate isolates the two tenants.
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) FROM enterprise_audit_events WHERE enterprise_id = $1 AND payload->>'employee_id' = $2::text")).
		WithArgs(int64(2), employeeID).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(0)))
	mock.ExpectQuery(regexp.QuoteMeta("WHERE enterprise_id = $1 AND payload->>'employee_id' = $2::text\n\t\tORDER BY created_at DESC, id DESC")).
		WithArgs(int64(2), employeeID, 20, 0).
		WillReturnRows(sqlmock.NewRows([]string{"id", "event_type", "entity_type", "entity_id", "payload", "actor_ref", "created_at"}))

	items, total, err = handler.listAuditEvents(context.Background(), 2, workbenchQuery{EmployeeID: &employeeID, Page: 1, PageSize: 20}, "", "")
	require.NoError(t, err)
	require.Equal(t, int64(0), total)
	require.Empty(t, items)
	require.NoError(t, mock.ExpectationsWereMet())
}
