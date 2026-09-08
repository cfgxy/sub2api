package routes

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/enterprise"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type enterpriseAllocationRouteStore struct {
	setParams    enterprise.SetAllocationParams
	summaryQuery enterprise.AllocationUsageSummaryQuery
	setErr       error
}

func (s *enterpriseAllocationRouteStore) SetAllocation(_ context.Context, params enterprise.SetAllocationParams) (*enterprise.Allocation, error) {
	s.setParams = params
	if s.setErr != nil {
		return nil, s.setErr
	}
	return &enterprise.Allocation{ID: 7, EnterpriseID: params.EnterpriseID, SubscriptionID: params.SubscriptionID, EmployeeID: params.EmployeeID, WindowType: params.WindowType, WindowAnchor: params.WindowAnchor, Credit: params.Credit, Version: 2}, nil
}

func (s *enterpriseAllocationRouteStore) GetAllocationUsageSummary(_ context.Context, query enterprise.AllocationUsageSummaryQuery) (*enterprise.AllocationUsageSummary, error) {
	s.summaryQuery = query
	limit := "4.00000000"
	return &enterprise.AllocationUsageSummary{
		ConfiguredCredit: "5.00000000", UsageCredit: "2.00000000", RemainingCredit: "3.00000000",
		OverageCredit: "0.00000000", AllocatedTotal: "6.00000000", AuthoritativeLimit: &limit,
		OverallocatedBy: "2.00000000", Warning: enterprise.AllocationWarningOverallocated,
	}, nil
}

func TestEnterpriseAllocationRoutesUseAuthenticatedSubjectAndProductionPaths(t *testing.T) {
	store := &enterpriseAllocationRouteStore{}
	router := newEnterpriseAllocationRouteTestRouter(store)

	unauthorized := performAllocationRouteRequest(router, http.MethodPut, "/api/v1/enterprise/subscriptions/11/allocations/22", validAllocationBody(), false)
	require.Equal(t, http.StatusUnauthorized, unauthorized.Code)

	success := performAllocationRouteRequest(router, http.MethodPut, "/api/v1/enterprise/subscriptions/11/allocations/22", validAllocationBody(), true)
	require.Equal(t, http.StatusOK, success.Code)
	require.Equal(t, int64(42), store.setParams.RequesterUserID)
	require.Equal(t, int64(9), store.setParams.EnterpriseID)
	require.Equal(t, int64(11), store.setParams.SubscriptionID)
	require.Equal(t, int64(22), store.setParams.EmployeeID)

	summary := performAllocationRouteRequest(router, http.MethodGet, "/api/v1/enterprise/subscriptions/11/allocations/22?enterprise_id=9&window_type=day&window_anchor=2026-09-08T00:00:00Z", nil, true)
	require.Equal(t, http.StatusOK, summary.Code)
	require.Equal(t, int64(42), store.summaryQuery.RequesterUserID)
	require.Equal(t, enterprise.WindowTypeDay, store.summaryQuery.WindowType)
	var response struct {
		Data enterprise.AllocationUsageSummary `json:"data"`
	}
	require.NoError(t, json.Unmarshal(summary.Body.Bytes(), &response))
	require.Equal(t, "2.00000000", response.Data.UsageCredit)
	require.Equal(t, "6.00000000", response.Data.AllocatedTotal)
	require.Equal(t, "4.00000000", *response.Data.AuthoritativeLimit)
	require.Equal(t, "2.00000000", response.Data.OverallocatedBy)
	require.Equal(t, enterprise.AllocationWarningOverallocated, response.Data.Warning)

	invalidSummary := performAllocationRouteRequest(router, http.MethodGet, "/api/v1/enterprise/subscriptions/11/allocations/22?enterprise_id=9&window_type=1d&window_anchor=2026-09-08T10:00:00Z", nil, true)
	require.Equal(t, http.StatusBadRequest, invalidSummary.Code)
}

func TestEnterpriseAllocationRoutesMapScopeVersionAndUnsafeReason(t *testing.T) {
	for name, tc := range map[string]struct {
		storeErr error
		body     []byte
		want     int
	}{
		"cross enterprise": {enterprise.ErrEnterpriseAccessDenied, validAllocationBody(), http.StatusForbidden},
		"version conflict": {enterprise.ErrAllocationVersionConflict, validAllocationBody(), http.StatusConflict},
		"secret reason":    {nil, []byte(`{"enterprise_id":9,"window_type":"day","window_anchor":"2026-09-08T00:00:00Z","credit":"5","reason":"token=secret"}`), http.StatusBadRequest},
		"long reason":      {nil, []byte(`{"enterprise_id":9,"window_type":"day","window_anchor":"2026-09-08T00:00:00Z","credit":"5","reason":"` + string(bytes.Repeat([]byte("a"), enterprise.MaxAllocationReasonLength+1)) + `"}`), http.StatusBadRequest},
	} {
		t.Run(name, func(t *testing.T) {
			store := &enterpriseAllocationRouteStore{setErr: tc.storeErr}
			router := newEnterpriseAllocationRouteTestRouter(store)
			response := performAllocationRouteRequest(router, http.MethodPut, "/api/v1/enterprise/subscriptions/11/allocations/22", tc.body, true)
			require.Equal(t, tc.want, response.Code)
		})
	}
}

func newEnterpriseAllocationRouteTestRouter(store handler.EnterpriseAllocationStore) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	v1 := router.Group("/api/v1")
	authenticated := v1.Group("")
	jwtAuth := middleware.JWTAuthMiddleware(func(c *gin.Context) {
		if c.GetHeader("Authorization") != "Bearer route-test" {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 42})
		c.Next()
	})
	authenticated.Use(gin.HandlerFunc(jwtAuth))
	RegisterEnterpriseAllocationRoutes(authenticated, handler.NewEnterpriseAllocationHandlerWithStore(store))
	return router
}

func performAllocationRouteRequest(router http.Handler, method, target string, body []byte, authenticated bool) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, target, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if authenticated {
		request.Header.Set("Authorization", "Bearer route-test")
	}
	router.ServeHTTP(recorder, request)
	return recorder
}

func validAllocationBody() []byte {
	return []byte(`{"enterprise_id":9,"window_type":"day","window_anchor":"2026-09-08T00:00:00Z","credit":"5","expected_version":1,"reason":"rebalance"}`)
}
