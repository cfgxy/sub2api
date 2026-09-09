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
