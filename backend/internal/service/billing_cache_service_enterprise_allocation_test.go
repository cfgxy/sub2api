package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type enterpriseAllocationRateLimitLoaderStub struct {
	mu    sync.RWMutex
	data  *APIKeyRateLimitData
	err   error
	calls int
}

func (s *enterpriseAllocationRateLimitLoaderStub) GetRateLimitData(context.Context, int64) (*APIKeyRateLimitData, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if s.data == nil {
		return nil, s.err
	}
	copy := *s.data
	if s.data.Window5hStart != nil {
		window := *s.data.Window5hStart
		copy.Window5hStart = &window
	}
	if s.data.Window1dStart != nil {
		window := *s.data.Window1dStart
		copy.Window1dStart = &window
	}
	if s.data.Window7dStart != nil {
		window := *s.data.Window7dStart
		copy.Window7dStart = &window
	}
	return &copy, s.err
}

func (s *enterpriseAllocationRateLimitLoaderStub) settleUsage7d(usage float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Usage7d = usage
}

func (s *enterpriseAllocationRateLimitLoaderStub) readCalls() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.calls
}

func TestEnterpriseEmployeeWeeklyAllocationOverageAdmission(t *testing.T) {
	anchor := time.Now().UTC()
	tests := []struct {
		name    string
		key     *APIKey
		usage   float64
		pending float64
		wantErr error
	}{
		{
			name: "enterprise reproduction amount is admitted within ten cent overage",
			key: &APIKey{
				ID: 1, EnterpriseEmployeeWeeklyLimit: true, RateLimit7d: 2,
				EnterpriseWeeklyWindowAnchor: &anchor,
			},
			usage: 1.631826, pending: 0.418595,
		},
		{
			name: "enterprise pending estimate is ignored while settled usage remains below tolerance",
			key: &APIKey{
				ID: 2, EnterpriseEmployeeWeeklyLimit: true, RateLimit7d: 2,
				EnterpriseWeeklyWindowAnchor: &anchor,
			},
			usage: 1.631826, pending: 99,
		},
		{
			name: "enterprise settled usage at tolerance boundary is admitted",
			key: &APIKey{
				ID: 3, EnterpriseEmployeeWeeklyLimit: true, RateLimit7d: 2,
				EnterpriseWeeklyWindowAnchor: &anchor,
			},
			usage: 2.1,
		},
		{
			name: "enterprise settled usage beyond tolerance is rejected",
			key: &APIKey{
				ID: 4, EnterpriseEmployeeWeeklyLimit: true, RateLimit7d: 2,
				EnterpriseWeeklyWindowAnchor: &anchor,
			},
			usage:   2.10000001,
			wantErr: ErrEnterpriseEmployeeWeeklyAllocationExceeded,
		},
		{
			name:    "ordinary API key retains its seven day limit",
			key:     &APIKey{ID: 5, RateLimit7d: 2},
			usage:   1.95,
			pending: 0.16,
			wantErr: ErrAPIKeyRateLimit7dExceeded,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &BillingCacheService{
				cfg: &config.Config{Billing: config.BillingConfig{
					EnterpriseEmployeeWeeklyAllocationOverageUSD: 0.10,
				}},
				apiKeyRateLimitLoader: &enterpriseAllocationRateLimitLoaderStub{data: &APIKeyRateLimitData{
					Usage7d: tt.usage, Window7dStart: &anchor,
				}},
			}

			err := svc.checkAPIKeyRateLimitsWithPending(context.Background(), tt.key, tt.pending)
			if tt.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestEnterpriseEmployeeConcurrentEligibilityUsesOnlySettledUsage(t *testing.T) {
	anchor := time.Now().UTC()
	loader := &enterpriseAllocationRateLimitLoaderStub{data: &APIKeyRateLimitData{
		Usage7d:       1.99,
		Window7dStart: &anchor,
	}}
	svc := &BillingCacheService{
		cfg: &config.Config{RunMode: config.RunModeSimple, Billing: config.BillingConfig{
			EnterpriseEmployeeWeeklyAllocationOverageUSD: 0.10,
		}},
		apiKeyRateLimitLoader: loader,
	}
	key := &APIKey{
		ID:                            73,
		EnterpriseEmployeeWeeklyLimit: true,
		RateLimit7d:                   2,
		EnterpriseWeeklyWindowAnchor:  &anchor,
	}

	const requests = 32
	start := make(chan struct{})
	errs := make(chan error, requests)
	var wg sync.WaitGroup
	for range requests {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if err := svc.CheckBillingEligibility(context.Background(), &User{ID: 9}, key, nil, nil, ""); err != nil {
				errs <- err
				return
			}
			// A legacy pending record, even when far above the allocation, must
			// not turn a settled-usage admission into a quota rejection.
			errs <- svc.checkAPIKeyRateLimitsWithPending(context.Background(), key, 99)
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.Equal(t, requests*2, loader.readCalls(), "each request must independently read settled usage")

	// Model a completed billing settlement that has committed usage above the
	// allocation plus configured tolerance. Only the next request is rejected.
	loader.settleUsage7d(2.10000001)
	err := svc.CheckBillingEligibility(context.Background(), &User{ID: 9}, key, nil, nil, "")
	require.ErrorIs(t, err, ErrEnterpriseEmployeeWeeklyAllocationExceeded)
}

func TestEnterpriseEmployeeWeeklyAllocationPreservesFiveHourAndDailyLimits(t *testing.T) {
	anchor := time.Now().UTC()
	for _, tt := range []struct {
		name    string
		key     *APIKey
		data    *APIKeyRateLimitData
		wantErr error
	}{
		{
			name:    "five hour limit remains enforced",
			key:     &APIKey{ID: 74, EnterpriseEmployeeWeeklyLimit: true, RateLimit5h: 1, RateLimit7d: 2, EnterpriseWeeklyWindowAnchor: &anchor},
			data:    &APIKeyRateLimitData{Usage5h: 1, Window5hStart: &anchor, Window7dStart: &anchor},
			wantErr: ErrAPIKeyRateLimit5hExceeded,
		},
		{
			name:    "daily limit remains enforced",
			key:     &APIKey{ID: 75, EnterpriseEmployeeWeeklyLimit: true, RateLimit1d: 1, RateLimit7d: 2, EnterpriseWeeklyWindowAnchor: &anchor},
			data:    &APIKeyRateLimitData{Usage1d: 1, Window1dStart: &anchor, Window7dStart: &anchor},
			wantErr: ErrAPIKeyRateLimit1dExceeded,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			svc := &BillingCacheService{apiKeyRateLimitLoader: &enterpriseAllocationRateLimitLoaderStub{data: tt.data}}
			err := svc.checkAPIKeyRateLimitsWithPending(context.Background(), tt.key, 0)
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestEnterpriseEmployeeWeeklyAllocationStateFailuresAreNotQuotaExhaustion(t *testing.T) {
	anchor := time.Now().UTC()
	key := &APIKey{ID: 1, EnterpriseEmployeeWeeklyLimit: true, RateLimit7d: 2, EnterpriseWeeklyWindowAnchor: &anchor}

	tests := []struct {
		name    string
		service *BillingCacheService
	}{
		{
			name:    "missing loader",
			service: &BillingCacheService{},
		},
		{
			name:    "loader failure",
			service: &BillingCacheService{apiKeyRateLimitLoader: &enterpriseAllocationRateLimitLoaderStub{err: errors.New("database unavailable")}},
		},
		{
			name: "window anchor mismatch",
			service: &BillingCacheService{apiKeyRateLimitLoader: &enterpriseAllocationRateLimitLoaderStub{data: &APIKeyRateLimitData{
				Window7dStart: rateLimitTimePtr(anchor.Add(-time.Hour)),
			}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.service.checkAPIKeyRateLimitsWithPending(context.Background(), key, 42)
			require.ErrorIs(t, err, ErrEnterpriseEmployeeWeeklyAllocationStateUnavailable)
			require.NotErrorIs(t, err, ErrEnterpriseEmployeeWeeklyAllocationExceeded)
		})
	}
}
