package repository

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestActiveSubscriptionLoaderDoesNotReferenceEnterpriseTables(t *testing.T) {
	source, err := os.ReadFile("user_subscription_repo.go")
	require.NoError(t, err)

	text := string(source)
	start := strings.Index(text, "func (r *userSubscriptionRepository) GetActiveByUserIDAndGroupID")
	require.NotEqual(t, -1, start)
	end := strings.Index(text[start:], "\nfunc (r *userSubscriptionRepository) Update")
	require.NotEqual(t, -1, end)
	loader := text[start : start+end]

	require.NotContains(t, loader, "enterprise_subscriptions")
	require.NotContains(t, loader, "enterprises")
}
