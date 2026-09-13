package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestApplyEnterpriseUsageAttributionUsesFrozenWindowAnchors(t *testing.T) {
	requestAt := time.Date(2026, 9, 13, 8, 0, 0, 0, time.UTC)
	frozenDaily := time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC)
	frozenWeekly := time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)
	frozenMonthly := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	staleAnchor := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	employeeID := int64(42)
	subscriptionID := int64(7)

	apiKey := &APIKey{
		ID:                             11,
		EnterpriseAttributionCandidate: true,
		EnterpriseAttributionIdentity: &EnterpriseUsageAttributionIdentity{
			EnterpriseID:             3,
			EnterpriseSubscriptionID: 9,
			UpstreamSubscriptionID:   subscriptionID,
			EmployeeID:               &employeeID,
			AssignmentGeneration:     4,
			Classification:           "employee",
			WindowAnchorsResolved:    true,
			DailyWindowAnchor:        &frozenDaily,
			WeeklyWindowAnchor:       &frozenWeekly,
			MonthlyWindowAnchor:      &frozenMonthly,
		},
	}
	subscription := &UserSubscription{
		ID:                 subscriptionID,
		DailyWindowStart:   &staleAnchor,
		WeeklyWindowStart:  &staleAnchor,
		MonthlyWindowStart: &staleAnchor,
	}
	log := &UsageLog{APIKeyID: apiKey.ID, UserID: 100}

	ApplyEnterpriseUsageAttribution(log, apiKey, subscription, requestAt)

	require.NotNil(t, log.EnterpriseAttribution)
	require.Equal(t, frozenDaily, *log.EnterpriseAttribution.DailyWindowAnchor)
	require.Equal(t, frozenWeekly, *log.EnterpriseAttribution.WeeklyWindowAnchor)
	require.Equal(t, frozenMonthly, *log.EnterpriseAttribution.MonthlyWindowAnchor)
	require.Equal(t, frozenDaily, *log.AttributionDailyWindowAnchor)
	require.Equal(t, frozenWeekly, *log.AttributionWeeklyWindowAnchor)
	require.Equal(t, frozenMonthly, *log.AttributionMonthlyWindowAnchor)
}

type enterpriseAttributionResolverStub struct {
	APIKeyRepository
	calls int
}

func (s *enterpriseAttributionResolverStub) ResolveEnterpriseUsageAttributionIdentity(
	context.Context,
	int64,
	string,
	*int64,
) (*EnterpriseUsageAttributionIdentity, error) {
	s.calls++
	return nil, nil
}

func TestFinalizeEnterpriseUsageAttributionWithoutSubscriptionIsNoop(t *testing.T) {
	resolver := &enterpriseAttributionResolverStub{}
	svc := NewAPIKeyService(resolver, nil, nil, nil, nil, nil, nil)
	apiKey := &APIKey{
		ID:                             11,
		EnterpriseAttributionCandidate: true,
		EnterpriseAttributionIdentity:  &EnterpriseUsageAttributionIdentity{EnterpriseID: 3},
	}

	require.NoError(t, svc.FinalizeEnterpriseUsageAttribution(context.Background(), apiKey, "redacted-credential", nil))
	require.Zero(t, resolver.calls)
	require.Nil(t, apiKey.EnterpriseAttributionIdentity)
}

func TestApplyEnterpriseUsageAttributionPreservesDeferredSnapshot(t *testing.T) {
	requestAt := time.Date(2026, 9, 13, 8, 0, 0, 0, time.UTC)
	frozenDaily := time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC)
	frozenWeekly := time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)
	frozenMonthly := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	employeeID := int64(42)
	oldSnapshot := &EnterpriseUsageAttributionSnapshot{
		EnterpriseID:         3,
		SubscriptionID:       9,
		EmployeeID:           &employeeID,
		AssignmentGeneration: 4,
		Classification:       "employee",
		RequestAt:            requestAt,
		DailyWindowAnchor:    &frozenDaily,
		WeeklyWindowAnchor:   &frozenWeekly,
		MonthlyWindowAnchor:  &frozenMonthly,
	}
	newEmployeeID := int64(99)
	newDaily := frozenDaily.Add(24 * time.Hour)
	newWeekly := frozenWeekly.Add(7 * 24 * time.Hour)
	newMonthly := frozenMonthly.Add(31 * 24 * time.Hour)
	apiKey := &APIKey{
		ID:                             11,
		EnterpriseAttributionCandidate: true,
		EnterpriseAttributionIdentity: &EnterpriseUsageAttributionIdentity{
			EnterpriseID:             3,
			EnterpriseSubscriptionID: 9,
			UpstreamSubscriptionID:   7,
			EmployeeID:               &newEmployeeID,
			AssignmentGeneration:     5,
			Classification:           "employee",
			WindowAnchorsResolved:    true,
			DailyWindowAnchor:        &newDaily,
			WeeklyWindowAnchor:       &newWeekly,
			MonthlyWindowAnchor:      &newMonthly,
		},
	}
	log := &UsageLog{EnterpriseAttribution: oldSnapshot}

	ApplyEnterpriseUsageAttribution(log, apiKey, &UserSubscription{ID: 7}, requestAt.Add(time.Hour))

	require.Equal(t, oldSnapshot, log.EnterpriseAttribution)
	require.True(t, log.EnterpriseAttributionCandidate)
	require.Equal(t, requestAt, log.AttributionRequestAt)
	require.Equal(t, frozenDaily, *log.AttributionDailyWindowAnchor)
	require.Equal(t, frozenWeekly, *log.AttributionWeeklyWindowAnchor)
	require.Equal(t, frozenMonthly, *log.AttributionMonthlyWindowAnchor)
}
