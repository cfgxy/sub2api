package enterpriseidentity

import (
	"net/http/httptest"
	"testing"
	"time"

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
