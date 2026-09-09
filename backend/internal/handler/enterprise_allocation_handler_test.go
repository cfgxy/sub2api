package handler

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/enterprise"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type enterpriseAllocationStoreStub struct {
	setParams    enterprise.SetAllocationParams
	setErr       error
	summaryErr   error
	calls        int
	summaryCalls int
}

func (s *enterpriseAllocationStoreStub) SetAllocation(_ context.Context, params enterprise.SetAllocationParams) (*enterprise.Allocation, error) {
	s.calls++
	s.setParams = params
	return &enterprise.Allocation{ID: 1, Version: 1, Amount: params.Amount}, s.setErr
}

func (s *enterpriseAllocationStoreStub) GetAllocationUsageSummary(context.Context, enterprise.AllocationUsageSummaryQuery) (*enterprise.AllocationUsageSummary, error) {
	s.summaryCalls++
	return &enterprise.AllocationUsageSummary{}, s.summaryErr
}

func TestEnterpriseAllocationHandlerRejectsUnauthenticatedRequest(t *testing.T) {
	store := &enterpriseAllocationStoreStub{}
	status := serveEnterpriseAllocationSet(t, store, false, `{"enterprise_id":1,"window_type":"day","window_anchor":"2026-09-08T00:00:00Z","credit":"1","reason":"test"}`)
	require.Equal(t, http.StatusUnauthorized, status)
	require.Zero(t, store.calls)
}

func TestEnterpriseAllocationHandlerEnforcesProtocolValidation(t *testing.T) {
	for name, body := range map[string]string{
		"invalid window type": `{"enterprise_id":1,"window_type":"7d","window_anchor":"2026-09-08T00:00:00Z","credit":"1","reason":"test"}`,
		"negative credit":     `{"enterprise_id":1,"window_type":"day","window_anchor":"2026-09-08T00:00:00Z","credit":"-1","reason":"test"}`,
		"legacy amount field": `{"enterprise_id":1,"window_type":"day","window_anchor":"2026-09-08T00:00:00Z","amount":"1","reason":"test"}`,
		"missing reason":      `{"enterprise_id":1,"window_type":"day","window_anchor":"2026-09-08T00:00:00Z","credit":"1"}`,
	} {
		t.Run(name, func(t *testing.T) {
			store := &enterpriseAllocationStoreStub{}
			status := serveEnterpriseAllocationSet(t, store, true, body)
			require.Equal(t, http.StatusBadRequest, status)
		})
	}
}

func TestEnterpriseAllocationHandlerPassesServerIdentityAndDeniesCrossScope(t *testing.T) {
	store := &enterpriseAllocationStoreStub{setErr: enterprise.ErrEnterpriseAccessDenied}
	status := serveEnterpriseAllocationSet(t, store, true, `{"enterprise_id":9,"window_type":"week","window_anchor":"2026-09-08T10:00:00Z","credit":"1","reason":"change"}`)
	require.Equal(t, http.StatusNotFound, status)
	require.Equal(t, int64(42), store.setParams.RequesterUserID)
	require.Equal(t, int64(9), store.setParams.EnterpriseID)
	require.Equal(t, int64(11), store.setParams.SubscriptionID)
	require.Equal(t, int64(22), store.setParams.EmployeeID)
	require.Equal(t, "1", store.setParams.Credit)
}

func TestEnterpriseAllocationSummaryHidesCrossScopeResource(t *testing.T) {
	store := &enterpriseAllocationStoreStub{summaryErr: enterprise.ErrEnterpriseAccessDenied}
	status := serveEnterpriseAllocationSummary(t, store, true)
	require.Equal(t, http.StatusNotFound, status)
	require.Equal(t, 1, store.summaryCalls)
}

func serveEnterpriseAllocationSet(t *testing.T, store EnterpriseAllocationStore, authenticated bool, body string) int {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	h := &EnterpriseAllocationHandler{store: store}
	router.PUT("/subscriptions/:subscription_id/allocations/:employee_id", func(c *gin.Context) {
		if authenticated {
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 42})
		}
		h.Set(c)
	})
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/subscriptions/11/allocations/22", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, req)
	return recorder.Code
}

func serveEnterpriseAllocationSummary(t *testing.T, store EnterpriseAllocationStore, authenticated bool) int {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	h := &EnterpriseAllocationHandler{store: store}
	router.GET("/subscriptions/:subscription_id/allocations/:employee_id", func(c *gin.Context) {
		if authenticated {
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 42})
		}
		h.Summary(c)
	})
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/subscriptions/11/allocations/22?enterprise_id=9&window_type=week&window_anchor=2026-09-08T10:00:00Z", nil)
	router.ServeHTTP(recorder, req)
	return recorder.Code
}
