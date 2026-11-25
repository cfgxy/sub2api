package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnterpriseIdentityMigrationContract(t *testing.T) {
	raw, err := FS.ReadFile("237_enterprise_identity.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(raw))

	for _, fragment := range []string{
		"enterprise_sessions",
		"enterprise_refresh_tokens",
		"consumed_at",
		"enterprise_password_reset_tokens",
		"enterprise_branding",
		"admin_user_id",
		"portal_host",
		"password_hash",
		"must_change_password",
		"initial_password_expires_at",
		"interval '24 hours'",
		"status = 'terminated'",
		"where status <> 'terminated'",
		"admin_user_id = dedicated_upstream_user_id",
		"background_content_type",
		"background_sha256",
		"unique (enterprise_id, id)",
	} {
		require.Contains(t, sql, fragment)
	}
}

func TestEnterpriseBrandObjectMigrationContract(t *testing.T) {
	raw, err := FS.ReadFile("238_enterprise_brand_object.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(raw))

	require.Contains(t, sql, "background_object_key")
	require.Contains(t, sql, "enterprise_name")
	require.Contains(t, sql, "enterprise_branding")
	require.Contains(t, sql, "background_content_type in ('image/jpeg', 'image/png', 'image/webp')")
	require.Contains(t, sql, "background_sha256 ~ '^[0-9a-f]{64}$'")
	require.Contains(t, sql, "background_size_bytes between 1 and 5242880")
	require.Contains(t, sql, "background_object_key = ''")
	require.Contains(t, sql, "set enterprise_name = enterprise.name")
	require.NotContains(t, sql, "alter column background_url")
}

func TestEnterpriseSubscriptionEndsWhenUpstreamSubscriptionIsRevoked(t *testing.T) {
	raw, err := FS.ReadFile("251_end_enterprise_subscription_on_upstream_revoke.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(raw))
	for _, fragment := range []string{
		"end_enterprise_subscriptions_on_upstream_revoke",
		"after update of deleted_at on user_subscriptions",
		"status = 'ended'",
		"upstream_user_subscription_id = new.id",
		"subscription.source_revoked",
	} {
		require.Contains(t, sql, fragment)
	}

	rollback, err := FS.ReadFile("rollback/251_end_enterprise_subscription_on_upstream_revoke.sql")
	require.NoError(t, err)
	require.Contains(t, strings.ToLower(string(rollback)), "drop trigger if exists trg_end_enterprise_subscription_on_upstream_revoke")
}

func TestEnterpriseEmployeeKeyLifecycleMigrationContract(t *testing.T) {
	raw, err := FS.ReadFile("239_enterprise_employee_key_lifecycle.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(raw))
	for _, fragment := range []string{
		"enterprise_key_lifecycle_idempotency",
		"request_hash char(64)",
		"result_api_key_id",
		"unique (enterprise_id, employee_id, operation, idempotency_key)",
		"references enterprise_employees (enterprise_id, id)",
	} {
		require.Contains(t, sql, fragment)
	}
	require.NotContains(t, sql, "response_body")
	require.NotContains(t, sql, "plaintext")

	rollback, err := FS.ReadFile("rollback/239_enterprise_employee_key_lifecycle.sql")
	require.NoError(t, err)
	require.Contains(t, strings.ToLower(string(rollback)), "drop table if exists enterprise_key_lifecycle_idempotency")
}
