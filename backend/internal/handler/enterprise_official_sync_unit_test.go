//go:build unit

package handler

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOfficialSyncWSTurnPreservesEnterpriseIdentity(t *testing.T) {
	groupID := int64(42)
	identity := &service.EnterpriseUsageAttributionIdentity{EnterpriseID: 11, EnterpriseSubscriptionID: 12, AssignmentGeneration: 3}
	conn := &service.APIKey{ID: 7, Key: "fixture", GroupID: &groupID,
		Group:                          &service.Group{ID: groupID, Platform: service.PlatformOpenAI, RateMultiplier: 1},
		EnterpriseAttributionCandidate: true, EnterpriseAttributionIdentity: identity}
	latest := *conn
	latest.Group = &service.Group{ID: groupID, Platform: service.PlatformOpenAI, RateMultiplier: 2}
	latest.EnterpriseAttributionIdentity = nil
	got := refreshOpenAIWSTurnBillingAPIKey(context.Background(), wsTurnAPIKeyLookupFunc(func(context.Context, string) (*service.APIKey, error) {
		return &latest, nil
	}), conn)
	require.Equal(t, float64(2), got.Group.RateMultiplier)
	require.True(t, got.EnterpriseAttributionCandidate)
	require.Same(t, identity, got.EnterpriseAttributionIdentity)
	var turns openAIWSTurnBillingAPIKeys
	turns.set(1, conn)
	turns.set(2, got)
	require.Same(t, conn, turns.forTurn(1, conn))
	require.Same(t, identity, turns.forTurn(2, conn).EnterpriseAttributionIdentity)
}

func TestOfficialSyncGeminiCacheChargePreservesEnterpriseSnapshot(t *testing.T) {
	for _, operation := range []string{"create", "patch"} {
		t.Run(operation, func(t *testing.T) {
			env := newCachedBillingEnv(t, 500, 900)
			employeeID := int64(21)
			anchor := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
			pricingAt := anchor.Add(time.Hour)
			env.apiKey.EnterpriseAttributionCandidate = true
			env.apiKey.EnterpriseAttributionIdentity = &service.EnterpriseUsageAttributionIdentity{
				EnterpriseID: 11, EnterpriseSubscriptionID: 12, UpstreamSubscriptionID: 13,
				EmployeeID: &employeeID, AssignmentGeneration: 3, Classification: "employee",
				WindowAnchorsResolved: true, WeeklyWindowAnchor: &anchor,
			}
			// 当前订阅窗口已经推进，费用归因仍须保留请求认证时的窗口。
			advanced := anchor.Add(7 * 24 * time.Hour)
			sub := &service.UserSubscription{ID: 13, WeeklyWindowStart: &advanced}
			requestID := "gcache:" + operation + ":fixture"
			env.serve(http.MethodPost, "/v1beta/cachedContents", "", nil, func(c *gin.Context) {
				env.h.submitGeminiCachedContentUsage(c, env.apiKey, env.accounts[900], sub,
					pricingAt, requestID, "gemini-2.5-pro", "gemini-2.5-pro", 10, 20,
					service.ChannelUsageFields{}, nil)
			})
			env.waitForUsage(t, 1)
			log := env.usage.first()
			require.Equal(t, requestID, log.RequestID)
			require.Positive(t, log.ActualCost)
			require.NotNil(t, log.EnterpriseAttribution)
			require.Equal(t, int64(11), log.EnterpriseAttribution.EnterpriseID)
			require.Equal(t, int64(12), log.EnterpriseAttribution.SubscriptionID)
			require.Equal(t, employeeID, *log.EnterpriseAttribution.EmployeeID)
			require.Equal(t, int64(3), log.EnterpriseAttribution.AssignmentGeneration)
			require.Equal(t, pricingAt, log.EnterpriseAttribution.RequestAt)
			require.Equal(t, anchor, *log.EnterpriseAttribution.WeeklyWindowAnchor)
		})
	}
}
