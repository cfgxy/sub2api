package service

import (
	"context"
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
			name: "enterprise amount below minus ten cents is rejected",
			key: &APIKey{
				ID: 2, EnterpriseEmployeeWeeklyLimit: true, RateLimit7d: 2,
				EnterpriseWeeklyWindowAnchor: &anchor,
			},
			usage: 1.631826, pending: 0.468175,
			wantErr: ErrEnterpriseEmployeeWeeklyAllocationExceeded,
		},
		{
			name:    "ordinary API key retains its seven day limit",
			key:     &APIKey{ID: 3, RateLimit7d: 2},
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
