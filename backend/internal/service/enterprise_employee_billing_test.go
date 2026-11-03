//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type enterpriseEmployeeBillingRepoStub struct {
	UsageBillingRepository
	applyCalls     int
	reserveCalls   int
	settleCalls    int
	releaseCalls   int
	lastReserve    *EnterpriseEmployeeUsageReservationCommand
	settleErr      error
	settleFailures int
}

func (s *enterpriseEmployeeBillingRepoStub) Apply(context.Context, *UsageBillingCommand) (*UsageBillingApplyResult, error) {
	s.applyCalls++
	return &UsageBillingApplyResult{Applied: s.applyCalls == 1}, nil
}

func (s *enterpriseEmployeeBillingRepoStub) ReserveEnterpriseEmployeeUsage(_ context.Context, cmd *EnterpriseEmployeeUsageReservationCommand) (*EnterpriseEmployeeUsageReservationResult, error) {
	s.reserveCalls++
	copy := *cmd
	s.lastReserve = &copy
	return &EnterpriseEmployeeUsageReservationResult{
		ReservationID:  1,
		RequestID:      cmd.RequestID,
		SourceAPIKeyID: cmd.SourceAPIKeyID,
		ReservedUSD:    cmd.ReservedUSD,
		Status:         "reserved",
	}, nil
}

func (s *enterpriseEmployeeBillingRepoStub) SettleEnterpriseEmployeeUsage(_ context.Context, cmd *EnterpriseEmployeeUsageSettlementCommand) (*EnterpriseEmployeeUsageReservationResult, error) {
	s.settleCalls++
	if s.settleFailures > 0 {
		s.settleFailures--
		return nil, errors.New("settlement temporarily unavailable")
	}
	if s.settleErr != nil {
		return nil, s.settleErr
	}
	actual := cmd.ActualUSD
	return &EnterpriseEmployeeUsageReservationResult{
		ReservationID:  1,
		RequestID:      cmd.RequestID,
		SourceAPIKeyID: cmd.SourceAPIKeyID,
		ActualUSD:      &actual,
		Status:         "settled",
	}, nil
}

func (s *enterpriseEmployeeBillingRepoStub) ReleaseEnterpriseEmployeeUsage(_ context.Context, cmd *EnterpriseEmployeeUsageReleaseCommand) (*EnterpriseEmployeeUsageReservationResult, error) {
	s.releaseCalls++
	return &EnterpriseEmployeeUsageReservationResult{
		ReservationID:  1,
		RequestID:      cmd.RequestID,
		SourceAPIKeyID: cmd.SourceAPIKeyID,
		Status:         "released",
	}, nil
}

func TestEnterpriseEmployeeReservationRequiresProvenBoundAndAcceptsExactCharge(t *testing.T) {
	cfg := &config.Config{}
	cfg.Billing.InflightReservation.DefaultMaxTokens = 8192
	cfg.Billing.InflightReservation.MaxInputTokens = 1_000_000
	cfg.Billing.InflightReservation.MaxOutputTokens = 128_000
	cfg.Billing.InflightReservation.MaxReservationUSD = 100
	billing := NewBillingService(cfg, nil)
	repo := &enterpriseEmployeeBillingRepoStub{}
	svc := &GatewayService{
		cfg: cfg, billingService: billing, resolver: NewModelPricingResolver(nil, billing),
		usageBillingRepo: repo,
	}
	apiKey := &APIKey{ID: 9, User: &User{ID: 10}, EnterpriseEmployeeWeeklyLimit: true, Group: &Group{RateMultiplier: 1}}

	_, err := svc.ReserveEnterpriseEmployeeUsage(context.Background(), apiKey, InflightEstimateRequest{
		Model: "claude-sonnet-4-5", BodyBytes: 100,
	})
	require.ErrorIs(t, err, ErrEnterpriseEmployeeMaximumCostUnknown)
	require.Zero(t, repo.reserveCalls, "configured estimator caps are not proof of the upstream output bound")

	reservation, err := svc.ReserveEnterpriseEmployeeFixedCharge(context.Background(), apiKey, 1.25)
	require.NoError(t, err)
	require.NotNil(t, reservation)
	require.Equal(t, 1, repo.reserveCalls)
	require.NotNil(t, repo.lastReserve)
	require.InDelta(t, 1.25, repo.lastReserve.ReservedUSD, 0.000000001)
}

func TestEnterpriseEmployeeSettlementRetriesAfterCoreBillingDedup(t *testing.T) {
	repo := &enterpriseEmployeeBillingRepoStub{settleErr: errors.New("settlement unavailable")}
	employee := newEnterpriseEmployeeUsageReservation(repo, &EnterpriseEmployeeUsageReservationResult{
		ReservationID: 1, RequestID: "employee-request", SourceAPIKeyID: 13, ReservedUSD: 4, Status: "reserved",
	})
	handle := AttachEnterpriseEmployeeUsageReservation(nil, employee)
	defer handle.HandlerDone()
	ctx := WithInflightReservation(context.Background(), handle)
	params := &postUsageBillingParams{
		Cost: &CostBreakdown{ActualCost: 3, TotalCost: 3}, User: &User{ID: 7},
		APIKey:  &APIKey{ID: 13, EnterpriseEmployeeWeeklyLimit: true},
		Account: &Account{ID: 9, Type: AccountTypeAPIKey},
	}
	deps := &billingDeps{deferredService: &DeferredService{}}

	applied, err := applyUsageBilling(ctx, "ignored-request-id", nil, params, deps, repo)
	require.True(t, applied)
	require.ErrorIs(t, err, ErrEnterpriseEmployeeSettlementAfterBillingCommit)
	applied, err = applyUsageBilling(ctx, "ignored-request-id", nil, params, deps, repo)
	require.False(t, applied)
	require.ErrorIs(t, err, ErrEnterpriseEmployeeSettlementAfterBillingCommit)
	require.Equal(t, 2, repo.applyCalls)
	require.Equal(t, 6, repo.settleCalls, "a deduplicated core billing retry must still retry employee settlement")
}

func TestEnterpriseEmployeeSettlementRetriesWithoutRepeatingCoreBilling(t *testing.T) {
	repo := &enterpriseEmployeeBillingRepoStub{settleFailures: 2}
	employee := newEnterpriseEmployeeUsageReservation(repo, &EnterpriseEmployeeUsageReservationResult{
		ReservationID: 1, RequestID: "employee-retry", SourceAPIKeyID: 13, ReservedUSD: 4, Status: "reserved",
	})
	handle := AttachEnterpriseEmployeeUsageReservation(nil, employee)
	defer handle.HandlerDone()
	params := &postUsageBillingParams{
		Cost: &CostBreakdown{ActualCost: 3, TotalCost: 3}, User: &User{ID: 7},
		APIKey:  &APIKey{ID: 13, EnterpriseEmployeeWeeklyLimit: true},
		Account: &Account{ID: 9, Type: AccountTypeAPIKey},
	}

	applied, err := applyUsageBilling(WithInflightReservation(context.Background(), handle), "ignored-request-id", nil, params, &billingDeps{deferredService: &DeferredService{}}, repo)
	require.NoError(t, err)
	require.True(t, applied)
	require.Equal(t, 1, repo.applyCalls, "core billing must commit exactly once")
	require.Equal(t, 3, repo.settleCalls, "only the independent employee transaction retries")
}

func TestRecordUsagePreservesActualCostWhenEmployeeSettlementFails(t *testing.T) {
	tests := []struct {
		name string
		run  func(context.Context, UsageLogRepository, UsageBillingRepository) error
	}{
		{
			name: "standard",
			run: func(ctx context.Context, logs UsageLogRepository, billingRepo UsageBillingRepository) error {
				svc := newGatewayRecordUsageServiceWithBillingRepoForTest(logs, billingRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{})
				return svc.RecordUsage(ctx, &RecordUsageInput{
					Result: &ForwardResult{RequestID: "employee-standard", Model: "claude-sonnet-4", Usage: ClaudeUsage{InputTokens: 100, OutputTokens: 20}},
					APIKey: &APIKey{ID: 13, EnterpriseEmployeeWeeklyLimit: true, Group: &Group{RateMultiplier: 1}},
					User:   &User{ID: 7}, Account: &Account{ID: 9, Type: AccountTypeAPIKey},
				})
			},
		},
		{
			name: "openai",
			run: func(ctx context.Context, logs UsageLogRepository, billingRepo UsageBillingRepository) error {
				svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(logs, billingRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
				return svc.RecordUsage(ctx, &OpenAIRecordUsageInput{
					Result: &OpenAIForwardResult{RequestID: "employee-openai", Model: "gpt-5.1", Usage: OpenAIUsage{InputTokens: 100, OutputTokens: 20}},
					APIKey: &APIKey{ID: 13, EnterpriseEmployeeWeeklyLimit: true, Group: &Group{RateMultiplier: 1}},
					User:   &User{ID: 7}, Account: &Account{ID: 9, Type: AccountTypeAPIKey},
				})
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			logs := &openAIRecordUsageLogRepoStub{inserted: true}
			repo := &enterpriseEmployeeBillingRepoStub{settleErr: errors.New("settlement unavailable")}
			employee := newEnterpriseEmployeeUsageReservation(repo, &EnterpriseEmployeeUsageReservationResult{
				ReservationID: 1, RequestID: "employee-" + test.name, SourceAPIKeyID: 13, ReservedUSD: 4, Status: "reserved",
			})
			handle := AttachEnterpriseEmployeeUsageReservation(nil, employee)
			defer handle.HandlerDone()
			ctx := WithInflightReservation(context.Background(), handle)

			err := test.run(ctx, logs, repo)
			require.ErrorIs(t, err, ErrEnterpriseEmployeeSettlementAfterBillingCommit)
			require.Equal(t, 1, repo.applyCalls)
			require.Equal(t, employeeSettlementMaxAttempts, repo.settleCalls)
			require.NotNil(t, logs.lastLog)
			require.Positive(t, logs.lastLog.ActualCost)
		})
	}
}
