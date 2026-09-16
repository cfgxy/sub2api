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

func TestParseWorkbenchQueryParsesModelFilter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/", func(c *gin.Context) {
		query, ok := parseWorkbenchQuery(c, true)
		require.True(t, ok)
		require.Equal(t, "gpt-4o", query.Model)
	})
	req := httptest.NewRequest("GET", "/?model=gpt-4o", nil)
	router.ServeHTTP(httptest.NewRecorder(), req)
}

func TestParseWorkbenchQueryRejectsInvalidWindowAndRange(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, target := range []string{
		"/?window_type=quarter",
		// day/month are exactly the values the standalone usage page's
		// window selector used to offer before B1 was fixed — the frontend
		// now only offers "week", but this guards the API contract itself.
		"/?window_type=day",
		"/?window_type=month",
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

// TestGetSummaryMarksOverageRecommendationByNumericSemantics 锁定员工 Recommendation 的数值语义判定：
// overage 字符串形态来自数据库 NUMERIC 定长输出（QA 实测同源列为 "0.00000000"），
// 禁止按字符串字面量与 "0" 比较——定长形态的零值会被误标「核对个人超用」。
// TestSanitizeAuditPayloadIsFailClosedWhitelist locks the detail-drawer payload
// rendering to an explicit field whitelist rather than a denylist: any field
// that is not on the known-safe list is dropped by default, even if it isn't
// named after a known-sensitive keyword. This protects against a future audit
// writer accidentally introducing a new secret-bearing field that a denylist
// wouldn't have anticipated.
func TestSanitizeAuditPayloadIsFailClosedWhitelist(t *testing.T) {
	payload := sanitizeAuditPayload([]byte(`{"result":"success","reason":"ok","employee_id":10,"unexpected_new_field":"leaked-value","internal_debug_dump":{"anything":"here"}}`))
	require.Equal(t, "success", payload["result"])
	require.Equal(t, "ok", payload["reason"])
	require.Equal(t, float64(10), payload["employee_id"])
	require.NotContains(t, payload, "unexpected_new_field")
	require.NotContains(t, payload, "internal_debug_dump")
}

func TestGetSummaryMarksOverageRecommendationByNumericSemantics(t *testing.T) {
	service, mock := newMockService(t)
	handler := &WorkbenchHandler{owner: &Handler{service: service}}

	mock.ExpectQuery(`SELECT COALESCE\(SUM\(COALESCE\(usage_log\.actual_cost, 0\)\), 0\)::text, COUNT\(\*\)`).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"total_usage_credit", "total_requests"}).AddRow("2.75", int64(3)))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM enterprise_employees`).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(2)))
	mock.ExpectQuery(`SELECT COUNT\(DISTINCT attribution\.employee_id\)`).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(2)))
	mock.ExpectQuery(`(?s)WITH employee_window_usage AS .*`).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"employee_id", "email", "department_id", "requests", "configured_credit", "usage_credit", "remaining_credit", "overage_credit"}).
			AddRow(int64(22), "employee-zero@example.com", int64(3), int64(3), "200.00000000", "50.00000000", "150.00000000", "0.00000000").
			AddRow(int64(23), "employee-over@example.com", int64(3), int64(2), "100.00000000", "150.00000000", "0", "50.0000000000"))
	mock.ExpectQuery(`SELECT enterprise_subscription\.id, CASE WHEN enterprise_subscription\.id IS NULL`).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"subscription_id", "source_status", "plan", "subscription_status", "pool_limit", "pool_used", "pool_remaining", "pool_exhausted", "pool_anchor"}).
			AddRow(int64(55), "available", "Business", "active", "550.00000000", "220.00000000", "330.00000000", false, time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)))
	mock.ExpectQuery(`(?s)SELECT observed_at\s+FROM enterprise_subscription_windows`).
		WithArgs(int64(7), int64(55)).
		WillReturnRows(sqlmock.NewRows([]string{"observed_at"}))
	mock.ExpectQuery(`(?s)FROM enterprise_subscriptions AS enterprise_subscription.*status = 'scheduled'`).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"plan", "created_at"}))
	mock.ExpectQuery(`SELECT DATE_TRUNC\('day', attribution\.request_at\)`).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"at", "requests", "usage_credit"}))

	result, err := handler.getSummary(t.Context(), 7, workbenchQuery{})
	require.NoError(t, err)
	require.Len(t, result.EmployeeSummaries, 2)
	require.Equal(t, "当前 allocation 范围内", result.EmployeeSummaries[0].Recommendation,
		"overage 定长零值 0.00000000 必须判为零，不得误标核对个人超用")
	require.Equal(t, "核对个人超用，并按业务需要调整 allocation", result.EmployeeSummaries[1].Recommendation)
	require.NoError(t, mock.ExpectationsWereMet())
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
		WillReturnRows(sqlmock.NewRows([]string{"employee_id", "email", "department_id", "requests", "configured_credit", "usage_credit", "remaining_credit", "overage_credit"}).
			AddRow(int64(22), "employee@example.com", int64(3), int64(3), "12.50", "2.75", "9.75", "0"))
	mock.ExpectQuery(`SELECT enterprise_subscription\.id, CASE WHEN enterprise_subscription\.id IS NULL`).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"subscription_id", "source_status", "plan", "subscription_status", "pool_limit", "pool_used", "pool_remaining", "pool_exhausted", "pool_anchor"}).
			AddRow(int64(55), "available", "Business", "active", "100", "2.75", "97.25", false, time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)))
	mock.ExpectQuery(`(?s)SELECT observed_at\s+FROM enterprise_subscription_windows`).
		WithArgs(int64(7), int64(55)).
		WillReturnRows(sqlmock.NewRows([]string{"observed_at"}).AddRow(time.Date(2026, 9, 8, 10, 32, 0, 0, time.UTC)))
	mock.ExpectQuery(`(?s)FROM enterprise_subscriptions AS enterprise_subscription.*status = 'scheduled'`).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"plan", "created_at"}))
	mock.ExpectQuery(`SELECT DATE_TRUNC\('day', attribution\.request_at\)`).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"at", "requests", "usage_credit"}).AddRow(time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC), int64(3), "2.75"))

	result, err := handler.getSummary(t.Context(), 7, workbenchQuery{})
	require.NoError(t, err)
	require.Equal(t, "2.75", result.TotalUsageCredit)
	require.Equal(t, int64(3), result.TotalRequests)
	require.Equal(t, "100", result.EnterprisePoolLimit)
	require.Equal(t, "97.25", result.EnterprisePoolRemaining)
	require.Len(t, result.EmployeeSummaries, 1)
	require.Equal(t, int64(3), result.EmployeeSummaries[0].Requests)
	require.Equal(t, "12.50", result.EmployeeSummaries[0].ConfiguredCredit)
	require.Equal(t, "9.75", result.EmployeeSummaries[0].RemainingCredit)
	require.Equal(t, "0", result.EmployeeSummaries[0].OverageCredit)
	require.Equal(t, "available", result.PoolSourceStatus)
	require.NotNil(t, result.SubscriptionID)
	require.Equal(t, int64(55), *result.SubscriptionID)
	require.NotNil(t, result.PoolObservedAt)
	require.Equal(t, time.Date(2026, 9, 8, 10, 32, 0, 0, time.UTC), *result.PoolObservedAt)
	require.Empty(t, result.ScheduledSubscriptionPlan)
	require.Nil(t, result.ScheduledSubscriptionSince)
	require.Len(t, result.UsageTrend, 1)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestGetSummarySurfacesScheduledSubscription(t *testing.T) {
	service, mock := newMockService(t)
	handler := &WorkbenchHandler{owner: &Handler{service: service}}

	mock.ExpectQuery(`SELECT COALESCE\(SUM\(COALESCE\(usage_log\.actual_cost, 0\)\), 0\)::text, COUNT\(\*\)`).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"total_usage_credit", "total_requests"}).AddRow("0", int64(0)))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM enterprise_employees`).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(0)))
	mock.ExpectQuery(`SELECT COUNT\(DISTINCT attribution\.employee_id\)`).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(0)))
	mock.ExpectQuery(`(?s)WITH employee_window_usage AS .*attribution\.window_type = 'week'.*allocation\.subscription_id = usage\.subscription_id.*allocation\.window_type = usage\.window_type.*allocation\.window_anchor = usage\.window_anchor`).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"employee_id", "email", "department_id", "requests", "configured_credit", "usage_credit", "remaining_credit", "overage_credit"}))
	mock.ExpectQuery(`SELECT enterprise_subscription\.id, CASE WHEN enterprise_subscription\.id IS NULL`).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"subscription_id", "source_status", "plan", "subscription_status", "pool_limit", "pool_used", "pool_remaining", "pool_exhausted", "pool_anchor"}).
			AddRow(nil, "unavailable", "", "", "0", "0", "0", false, nil))
	mock.ExpectQuery(`(?s)FROM enterprise_subscriptions AS enterprise_subscription.*status = 'scheduled'`).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"plan", "created_at"}).AddRow("Business", time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC)))
	mock.ExpectQuery(`SELECT DATE_TRUNC\('day', attribution\.request_at\)`).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"at", "requests", "usage_credit"}))

	result, err := handler.getSummary(t.Context(), 7, workbenchQuery{})
	require.NoError(t, err)
	require.Nil(t, result.SubscriptionID)
	require.Nil(t, result.PoolObservedAt)
	require.Equal(t, "Business", result.ScheduledSubscriptionPlan)
	require.NotNil(t, result.ScheduledSubscriptionSince)
	require.Equal(t, time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC), *result.ScheduledSubscriptionSince)
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
		WillReturnRows(sqlmock.NewRows([]string{"id", "event_type", "entity_type", "entity_id", "result", "reason", "payload", "actor_ref", "created_at"}).
			AddRow(int64(99), "employee.terminated", "employee", employeeID, "success", "", []byte(`{"employee_id":10}`), "enterprise_admin:1", terminatedAt))

	items, total, err := handler.listAuditEvents(context.Background(), 1, workbenchQuery{EmployeeID: &employeeID, Page: 1, PageSize: 20}, "", "", "", "", "", "")
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
		WillReturnRows(sqlmock.NewRows([]string{"id", "event_type", "entity_type", "entity_id", "result", "reason", "payload", "actor_ref", "created_at"}))

	items, total, err = handler.listAuditEvents(context.Background(), 2, workbenchQuery{EmployeeID: &employeeID, Page: 1, PageSize: 20}, "", "", "", "", "", "")
	require.NoError(t, err)
	require.Equal(t, int64(0), total)
	require.Empty(t, items)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestListUsageFiltersByModel(t *testing.T) {
	service, mock := newMockService(t)
	handler := &WorkbenchHandler{owner: &Handler{service: service}}
	query := workbenchQuery{Page: 1, PageSize: 20, Model: "gpt-4o"}

	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM enterprise_usage_attributions AS attribution.*usage_log\.model = \$2`).
		WithArgs(int64(7), "gpt-4o").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(1)))
	mock.ExpectQuery(`(?s)SELECT attribution\.id.*usage_log\.model = \$2.*ORDER BY attribution\.request_at DESC`).
		WithArgs(int64(7), "gpt-4o", 20, 0).
		WillReturnRows(sqlmock.NewRows([]string{
			"attribution_id", "usage_log_id", "employee_id", "employee_email", "department_id",
			"api_key_id", "api_key_masked", "model", "window_type", "window_anchor", "request_at",
			"classification", "assignment_generation", "usage_credit", "configured_credit",
		}).AddRow(int64(1), int64(2), int64(22), "employee@example.com", int64(3), int64(9),
			"sk-abc...1234", "gpt-4o", "week", time.Now(), time.Now(), "employee", int64(1), "0.50", "10"))

	items, total, err := handler.listUsage(t.Context(), 7, query)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, items, 1)
	require.Equal(t, "gpt-4o", items[0].Model)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestListAuditEventsFiltersOperatorResultReasonAndSearch(t *testing.T) {
	service, mock := newMockService(t)
	handler := &WorkbenchHandler{owner: &Handler{service: service}}
	query := workbenchQuery{Page: 1, PageSize: 20, WindowType: "week"}
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM enterprise_audit_events WHERE`).
		WithArgs(int64(7), "week", "enterprise_admin", "failure", "disabled", "rotate").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(1)))
	mock.ExpectQuery(`(?s)SELECT id, event_type, entity_type, entity_id,.*COALESCE\(payload->>'result'`).
		WithArgs(int64(7), "week", "enterprise_admin", "failure", "disabled", "rotate", 20, 0).
		WillReturnRows(sqlmock.NewRows([]string{"id", "event_type", "entity_type", "entity_id", "result", "reason", "payload", "actor_ref", "created_at"}).
			AddRow(int64(1), "key.rotate", "api_key", int64(9), "failure", "disabled", []byte(`{"reason":"disabled"}`), "enterprise_admin", time.Now()))

	items, total, err := handler.listAuditEvents(t.Context(), 7, query, "", "", "enterprise_admin", "failure", "disabled", "rotate")
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, items, 1)
	require.Equal(t, "failure", items[0].Result)
	require.Equal(t, "disabled", items[0].Reason)
	require.NoError(t, mock.ExpectationsWereMet())
}
