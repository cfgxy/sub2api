package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAPIKeyAuthSnapshotPreservesEnterpriseAttributionCandidate(t *testing.T) {
	svc := &APIKeyService{}
	apiKey := &APIKey{
		ID: 11, UserID: 22, EnterpriseAttributionCandidate: true,
		User: &User{ID: 22, Status: StatusActive},
	}

	snapshot := svc.snapshotFromAPIKey(context.Background(), apiKey)
	require.NotNil(t, snapshot)
	require.True(t, snapshot.EnterpriseAttributionCandidate)
	require.Equal(t, 24, snapshot.Version)

	materialized := svc.snapshotToAPIKey("sk-test", snapshot)
	require.True(t, materialized.EnterpriseAttributionCandidate)
}
