//go:build integration

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestOrdinarySubscriptionUsageDoesNotRequireEnterpriseTableAccess(t *testing.T) {
	ctx := context.Background()
	suffix := time.Now().UnixNano()
	roleName := fmt.Sprintf("shan154_ordinary_%d", suffix)
	password := fmt.Sprintf("shan154-test-%d", suffix)
	quotedRole := pq.QuoteIdentifier(roleName)

	_, err := integrationDB.ExecContext(ctx, fmt.Sprintf("CREATE ROLE %s LOGIN PASSWORD %s",
		quotedRole, pq.QuoteLiteral(password)))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), fmt.Sprintf("DROP OWNED BY %s", quotedRole))
		_, _ = integrationDB.ExecContext(context.Background(), fmt.Sprintf("DROP ROLE IF EXISTS %s", quotedRole))
	})
	_, err = integrationDB.ExecContext(ctx, fmt.Sprintf(`
		GRANT USAGE ON SCHEMA public TO %[1]s;
		GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO %[1]s;
		GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO %[1]s;
		GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA public TO %[1]s;
		REVOKE ALL ON enterprises, enterprise_subscriptions FROM %[1]s;
	`, quotedRole))
	require.NoError(t, err)

	var userID, groupID, subscriptionID, apiKeyID, accountID int64
	keyValue := fmt.Sprintf("sk-ordinary-isolation-%d", suffix)
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		"INSERT INTO users (email, password_hash) VALUES ($1, 'test') RETURNING id",
		fmt.Sprintf("ordinary-isolation-%d@example.com", suffix)).Scan(&userID))
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		"INSERT INTO groups (name, subscription_type) VALUES ($1, 'subscription') RETURNING id",
		fmt.Sprintf("ordinary-isolation-%d", suffix)).Scan(&groupID))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		INSERT INTO user_subscriptions (user_id, group_id, starts_at, expires_at, status)
		VALUES ($1, $2, NOW() - INTERVAL '1 hour', NOW() + INTERVAL '1 day', 'active') RETURNING id
	`, userID, groupID).Scan(&subscriptionID))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		INSERT INTO api_keys (user_id, group_id, key, name)
		VALUES ($1, $2, $3, 'ordinary-isolation') RETURNING id
	`, userID, groupID, keyValue).Scan(&apiKeyID))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		INSERT INTO accounts (name, platform, type) VALUES ($1, 'anthropic', 'apikey') RETURNING id
	`, fmt.Sprintf("ordinary-isolation-%d", suffix)).Scan(&accountID))
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM usage_logs WHERE api_key_id = $1", apiKeyID)
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM api_keys WHERE id = $1", apiKeyID)
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM auth_cache_invalidation_outbox WHERE cache_key = encode(sha256(convert_to($1, 'UTF8')), 'hex')", keyValue)
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM user_subscriptions WHERE id = $1", subscriptionID)
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM accounts WHERE id = $1", accountID)
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM groups WHERE id = $1", groupID)
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM users WHERE id = $1", userID)
	})

	restrictedDSN, err := url.Parse(integrationDSN)
	require.NoError(t, err)
	restrictedDSN.User = url.UserPassword(roleName, password)
	restrictedDB, err := sql.Open("postgres", restrictedDSN.String())
	require.NoError(t, err)
	t.Cleanup(func() { _ = restrictedDB.Close() })
	require.NoError(t, restrictedDB.PingContext(ctx))
	restrictedEnt := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, restrictedDB)))
	t.Cleanup(func() { _ = restrictedEnt.Close() })

	apiKey, err := NewAPIKeyRepository(restrictedEnt, restrictedDB).GetByKeyForAuth(ctx, keyValue)
	require.NoError(t, err)
	require.False(t, apiKey.EnterpriseAttributionCandidate)
	subscription, err := NewUserSubscriptionRepository(restrictedEnt).GetActiveByUserIDAndGroupID(ctx, userID, groupID)
	require.NoError(t, err)
	require.Equal(t, subscriptionID, subscription.ID)

	inserted, err := NewUsageLogRepository(nil, restrictedDB).Create(ctx, &service.UsageLog{
		UserID: userID, APIKeyID: apiKeyID, AccountID: accountID,
		GroupID: &groupID, SubscriptionID: &subscriptionID,
		Model: "ordinary-isolation", ActualCost: 1, CreatedAt: time.Now(),
		EnterpriseAttributionCandidate: false,
	})
	require.NoError(t, err)
	require.True(t, inserted)
}
