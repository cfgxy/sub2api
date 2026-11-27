package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type enterpriseAllocationRateLimitLoaderStub struct {
	data *APIKeyRateLimitData
	err  error
}

func (s *enterpriseAllocationRateLimitLoaderStub) GetRateLimitData(context.Context, int64) (*APIKeyRateLimitData, error) {
	return s.data, s.err
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
