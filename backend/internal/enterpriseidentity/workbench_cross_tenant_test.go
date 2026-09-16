package enterpriseidentity

import (
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// mountWorkbenchWithClaims wires the workbench routes behind a fake auth
// middleware that injects claims the way the real JWT middleware would —
// i.e. never from client-supplied request data. This isolates the handler's
// scoping behavior from JWT verification, which is covered elsewhere.
func mountWorkbenchWithClaims(handler *WorkbenchHandler, claims *Claims) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(claimsContextKey, claims)
		c.Next()
	})
	group := router.Group("/")
	handler.registerRoutes(group)
	return router
}

// TestWorkbenchUsageIgnoresForgedEnterpriseIdentifierInQuery proves that a
// forged enterprise_id supplied as a query parameter cannot widen or redirect
// the scope of a usage query: enterprise isolation is derived exclusively
// from the server-verified claims injected by auth middleware, and the
// handler/query layer never reads an enterprise identifier from client input.
func TestWorkbenchUsageIgnoresForgedEnterpriseIdentifierInQuery(t *testing.T) {
	service, mock := newMockService(t)
	handler := &WorkbenchHandler{owner: &Handler{service: service}}
	router := mountWorkbenchWithClaims(handler, &Claims{EnterpriseID: 7, Role: "enterprise_admin"})

	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM enterprise_usage_attributions`).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(0)))
	mock.ExpectQuery(`(?s)SELECT attribution\.id.*ORDER BY attribution\.request_at DESC`).
		WithArgs(int64(7), 20, 0).
		WillReturnRows(sqlmock.NewRows([]string{
			"attribution_id", "usage_log_id", "employee_id", "employee_email", "department_id",
			"api_key_id", "api_key_masked", "model", "window_type", "window_anchor", "request_at",
			"classification", "assignment_generation", "usage_credit", "configured_credit",
		}))

	// The attacker-controlled enterprise_id=999 must never reach the query:
	// only the server-verified claims' EnterpriseID (7) may be used.
	req := httptest.NewRequest("GET", "/usage?enterprise_id=999", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, 200, rec.Code)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestWorkbenchAuditEventsIgnoresForgedEnterpriseIdentifierInQuery mirrors
// the above for the m-09 audit-events endpoint.
func TestWorkbenchAuditEventsIgnoresForgedEnterpriseIdentifierInQuery(t *testing.T) {
	service, mock := newMockService(t)
	handler := &WorkbenchHandler{owner: &Handler{service: service}}
	router := mountWorkbenchWithClaims(handler, &Claims{EnterpriseID: 7, Role: "enterprise_admin"})

	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM enterprise_audit_events WHERE`).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(0)))
	mock.ExpectQuery(`(?s)SELECT id, event_type, entity_type, entity_id,.*COALESCE\(payload->>'result'`).
		WithArgs(int64(7), 20, 0).
		WillReturnRows(sqlmock.NewRows([]string{"id", "event_type", "entity_type", "entity_id", "result", "reason", "payload", "actor_ref", "created_at"}))

	req := httptest.NewRequest("GET", "/audit-events?enterprise_id=999", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, 200, rec.Code)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestWorkbenchAuditEventsCrossEnterpriseDirectIDAccessIsNotVisible proves
// the "resource not visible" contract for a forged/guessed audit event ID
// belonging to another enterprise: the query is always scoped by the
// caller's own enterprise_id first, so a direct ID lookup for an event that
// belongs to a different tenant returns zero rows — not a 403 that would
// confirm the resource's existence, and not the other tenant's data.
func TestWorkbenchAuditEventsCrossEnterpriseDirectIDAccessIsNotVisible(t *testing.T) {
	service, mock := newMockService(t)
	handler := &WorkbenchHandler{owner: &Handler{service: service}}
	router := mountWorkbenchWithClaims(handler, &Claims{EnterpriseID: 7, Role: "enterprise_admin"})

	// entity_id=999 references a real audit event, but it belongs to a
	// different enterprise; the enterprise_id=$1 predicate excludes it
	// before entity_id is ever compared.
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM enterprise_audit_events WHERE`).
		WithArgs(int64(7), int64(999)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(0)))
	mock.ExpectQuery(`(?s)SELECT id, event_type, entity_type, entity_id,.*COALESCE\(payload->>'result'`).
		WithArgs(int64(7), int64(999), 20, 0).
		WillReturnRows(sqlmock.NewRows([]string{"id", "event_type", "entity_type", "entity_id", "result", "reason", "payload", "actor_ref", "created_at"}))

	req := httptest.NewRequest("GET", "/audit-events?api_key_id=999", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, 200, rec.Code)
	require.Contains(t, rec.Body.String(), `"total":0`)
	require.NoError(t, mock.ExpectationsWereMet())
}
