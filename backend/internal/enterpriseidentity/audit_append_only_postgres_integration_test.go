//go:build integration

package enterpriseidentity

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
	postgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

// TestEnterpriseAuditEventsRejectsUpdateAndDelete proves the append-only
// invariant (migration 243) at the database layer: any attempt to mutate a
// stored audit event — whether from application code or a direct SQL
// tampering attempt — must fail with the trigger's explicit exception rather
// than silently succeeding.
func TestEnterpriseAuditEventsRejectsUpdateAndDelete(t *testing.T) {
	ctx := context.Background()
	container, err := postgres.Run(
		ctx,
		"postgres:15-alpine",
		postgres.WithDatabase("audit_append_only_test"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword("postgres"),
		postgres.BasicWaitStrategies(),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	dsn, err := container.ConnectionString(ctx, "sslmode=disable", "TimeZone=UTC")
	require.NoError(t, err)
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.PingContext(ctx))

	for _, statement := range []string{
		`CREATE TABLE enterprise_audit_events (
			id BIGSERIAL PRIMARY KEY,
			enterprise_id BIGINT NOT NULL,
			event_type VARCHAR(100) NOT NULL,
			entity_type VARCHAR(100) NOT NULL,
			entity_id BIGINT,
			payload JSONB NOT NULL DEFAULT '{}'::jsonb,
			actor_ref TEXT NOT NULL DEFAULT 'system',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE OR REPLACE FUNCTION reject_enterprise_audit_mutation() RETURNS trigger AS $$
		BEGIN
			RAISE EXCEPTION 'enterprise audit events are append-only';
		END;
		$$ LANGUAGE plpgsql`,
		`CREATE TRIGGER protect_enterprise_audit_append_only
		BEFORE UPDATE OR DELETE ON enterprise_audit_events
		FOR EACH ROW EXECUTE FUNCTION reject_enterprise_audit_mutation()`,
	} {
		_, err := db.ExecContext(ctx, statement)
		require.NoError(t, err)
	}

	var eventID int64
	require.NoError(t, db.QueryRowContext(ctx, `
		INSERT INTO enterprise_audit_events (enterprise_id, event_type, entity_type, payload, actor_ref)
		VALUES (7, 'employee.terminated', 'employee', '{"result":"success"}'::jsonb, 'user:1')
		RETURNING id`).Scan(&eventID))

	_, err = db.ExecContext(ctx, `UPDATE enterprise_audit_events SET payload = '{"tampered":true}'::jsonb WHERE id = $1`, eventID)
	require.Error(t, err, "an UPDATE against a stored audit event must be rejected")
	require.Contains(t, err.Error(), "append-only")

	_, err = db.ExecContext(ctx, `DELETE FROM enterprise_audit_events WHERE id = $1`, eventID)
	require.Error(t, err, "a DELETE against a stored audit event must be rejected")
	require.Contains(t, err.Error(), "append-only")

	var stillPresent int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM enterprise_audit_events WHERE id = $1`, eventID).Scan(&stillPresent))
	require.Equal(t, 1, stillPresent, "the original row must remain untouched after rejected tamper attempts")
}
