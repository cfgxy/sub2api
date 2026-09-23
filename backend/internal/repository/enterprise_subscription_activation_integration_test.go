//go:build integration

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/enterprise"
	"github.com/Wei-Shaw/sub2api/internal/enterpriseidentity"
	"github.com/stretchr/testify/require"
)

// seedEnterpriseActivationUpstream seeds the dedicated upstream user, an active
// group and the native subscription a platform enterprise binds to. A nil
// weeklyWindowStart models an upstream subscription without an established
// weekly window, which an active enterprise subscription cannot mirror.
func seedEnterpriseActivationUpstream(t *testing.T, ctx context.Context, suffix string, weeklyWindowStart *time.Time) (int64, int64, int64) {
	t.Helper()
	var userID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		INSERT INTO users (email, password_hash) VALUES ($1, 'test') RETURNING id
	`, "enterprise-activation-"+suffix+"@example.com").Scan(&userID))

	var groupID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		INSERT INTO groups (name, daily_limit_usd, weekly_limit_usd, monthly_limit_usd)
		VALUES ($1, 10, 20, 30) RETURNING id
	`, "enterprise-activation-group-"+suffix).Scan(&groupID))

	startsAt := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	var userSubscriptionID int64
	if weeklyWindowStart == nil {
		require.NoError(t, integrationDB.QueryRowContext(ctx, `
			INSERT INTO user_subscriptions (user_id, group_id, starts_at, expires_at, status)
			VALUES ($1, $2, $3::timestamptz, $3::timestamptz + INTERVAL '1 year', 'active') RETURNING id
		`, userID, groupID, startsAt).Scan(&userSubscriptionID))
	} else {
		require.NoError(t, integrationDB.QueryRowContext(ctx, `
			INSERT INTO user_subscriptions (
				user_id, group_id, starts_at, expires_at, status,
				daily_window_start, weekly_window_start, monthly_window_start
			) VALUES ($1, $2, $3::timestamptz, $3::timestamptz + INTERVAL '1 year', 'active',
			          $3::timestamptz, $4::timestamptz, $3::timestamptz) RETURNING id
		`, userID, groupID, startsAt, weeklyWindowStart).Scan(&userSubscriptionID))
	}
	return userID, groupID, userSubscriptionID
}

func cleanupEnterpriseActivationUpstream(t *testing.T, userID int64) {
	t.Helper()
	ctx := context.Background()
	_, err := integrationDB.ExecContext(ctx, `
		WITH deleted AS (
			DELETE FROM user_subscriptions WHERE user_id = $1 RETURNING group_id
		)
		DELETE FROM groups WHERE id IN (SELECT group_id FROM deleted)`, userID)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, userID)
	require.NoError(t, err)
}

func TestEnterpriseSubscriptionActivatesOnPlatformEnterpriseCreation(t *testing.T) {
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	anchor := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	userID, _, upstreamSubscriptionID := seedEnterpriseActivationUpstream(t, ctx, suffix, &anchor)

	svc := enterpriseidentity.NewService(integrationDB, nil, nil, nil, nil)
	created, err := svc.CreateEnterprise(ctx, enterpriseidentity.CreateEnterpriseInput{
		Name:                  "Acme " + suffix,
		Host:                  "acme-activation-" + suffix + ".example.com",
		DedicatedUpstreamUser: userID,
		Reason:                "shan-322 activation chain",
	}, 1)
	require.NoError(t, err)
	t.Cleanup(func() { cleanupEnterpriseFixture(t, created.ID, userID, suffix) })

	// No manual SQL: the enterprise owns an active subscription row right after
	// creation, mirroring the upstream native subscription window anchor.
	var upstreamRef int64
	var status string
	var activatedAt, endedAt, observed sql.NullTime
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT upstream_user_subscription_id, status, activated_at, ended_at, observed_weekly_window_start
		FROM enterprise_subscriptions WHERE enterprise_id = $1`, created.ID).
		Scan(&upstreamRef, &status, &activatedAt, &endedAt, &observed))
	require.Equal(t, upstreamSubscriptionID, upstreamRef)
	require.Equal(t, "active", status)
	require.False(t, endedAt.Valid)
	require.True(t, activatedAt.Valid)
	require.True(t, observed.Valid)
	require.True(t, observed.Time.UTC().Equal(anchor))

	// Append-only audit trail records both creation and activation.
	var auditCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM enterprise_audit_events
		WHERE enterprise_id = $1 AND event_type IN ('enterprise.created', 'subscription.activated')`,
		created.ID).Scan(&auditCount))
	require.Equal(t, 2, auditCount)

	// Downstream readiness: the enterprise pool is readable with the mirrored
	// upstream anchor.
	pool, err := svc.GetEnterprisePoolStatus(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, "available", pool.SourceStatus)
	require.NotNil(t, pool.WindowAnchor)
	require.True(t, pool.WindowAnchor.UTC().Equal(anchor))

	// Downstream readiness: employee key creation resolves the active row.
	var employeeID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		INSERT INTO enterprise_employees (enterprise_id, email, current_email, password_hash, status)
		VALUES ($1, $2, $2, '$2a$12$abcdefghijklmnopqrstuuuuuuuuuuuuuuuuuuuuuuuuuuuu', 'active') RETURNING id`,
		created.ID, "member-activation-"+suffix+"@example.com").Scan(&employeeID))
	repo := enterprise.NewRepository(integrationDB, enterpriseNoopAuthCacheInvalidator{})
	plaintext := "sk-enterprise-activation-" + suffix
	keyResult, err := repo.CreateEmployeeKey(ctx, enterprise.EmployeeKeyMutationParams{
		EnterpriseID: created.ID, EmployeeID: employeeID,
		IdempotencyKey: "shan-322-activation", Plaintext: plaintext, ActorRef: "test:shan-322",
	})
	require.NoError(t, err)
	require.False(t, keyResult.Replayed)
	require.Equal(t, plaintext, keyResult.Plaintext)
}

func TestEnterpriseCreationRollsBackWholeTransactionWhenActivationFails(t *testing.T) {
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	anchor := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	userID, _, upstreamSubscriptionID := seedEnterpriseActivationUpstream(t, ctx, suffix, &anchor)
	t.Cleanup(func() { cleanupEnterpriseActivationUpstream(t, userID) })

	functionName := fmt.Sprintf("fail_enterprise_activation_%s_fn", suffix)
	triggerName := fmt.Sprintf("fail_enterprise_activation_%s", suffix)
	_, err := integrationDB.ExecContext(ctx, fmt.Sprintf(`
		CREATE FUNCTION %s() RETURNS trigger AS $$
		BEGIN
			RAISE EXCEPTION 'forced enterprise activation failure';
		END
		$$ LANGUAGE plpgsql;
		CREATE TRIGGER %s BEFORE INSERT ON enterprise_subscriptions
		FOR EACH ROW EXECUTE FUNCTION %s();`, functionName, triggerName, functionName))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), fmt.Sprintf("DROP TRIGGER IF EXISTS %s ON enterprise_subscriptions", triggerName))
		_, _ = integrationDB.ExecContext(context.Background(), fmt.Sprintf("DROP FUNCTION IF EXISTS %s()", functionName))
	})

	svc := enterpriseidentity.NewService(integrationDB, nil, nil, nil, nil)
	_, err = svc.CreateEnterprise(ctx, enterpriseidentity.CreateEnterpriseInput{
		Name:                  "Acme Rollback " + suffix,
		Host:                  "acme-rollback-" + suffix + ".example.com",
		DedicatedUpstreamUser: userID,
		Reason:                "shan-322 rollback proof",
	}, 1)
	require.ErrorContains(t, err, "forced enterprise activation failure")

	// No half state: the enterprise and its audit trail rolled back together with
	// the failed activation insert.
	var enterpriseCount, subscriptionCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM enterprises WHERE portal_host = $1`,
		"acme-rollback-"+suffix+".example.com").Scan(&enterpriseCount))
	require.Zero(t, enterpriseCount)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM enterprise_subscriptions WHERE upstream_user_subscription_id = $1`,
		upstreamSubscriptionID).Scan(&subscriptionCount))
	require.Zero(t, subscriptionCount)
}

func TestEnterpriseCreationRejectsUpstreamSubscriptionWithoutWeeklyWindow(t *testing.T) {
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	userID, _, _ := seedEnterpriseActivationUpstream(t, ctx, suffix, nil)
	t.Cleanup(func() { cleanupEnterpriseActivationUpstream(t, userID) })

	svc := enterpriseidentity.NewService(integrationDB, nil, nil, nil, nil)
	_, err := svc.CreateEnterprise(ctx, enterpriseidentity.CreateEnterpriseInput{
		Name:                  "Acme NoAnchor " + suffix,
		Host:                  "acme-no-anchor-" + suffix + ".example.com",
		DedicatedUpstreamUser: userID,
		Reason:                "shan-322 anchor guard",
	}, 1)
	require.ErrorContains(t, err, "no active subscription")

	var enterpriseCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM enterprises WHERE portal_host = $1`,
		"acme-no-anchor-"+suffix+".example.com").Scan(&enterpriseCount))
	require.Zero(t, enterpriseCount)
}
