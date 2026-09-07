-- SHAN-151 enterprise foundation source preflight.
-- Databases that already recorded migration 235 predate this guard and may
-- safely backfill its migration record without revalidating created tables.

DO $$
DECLARE
    target_table TEXT;
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM schema_migrations
        WHERE filename = '235_enterprise_foundation.sql'
    ) THEN
        FOREACH target_table IN ARRAY ARRAY[
            'enterprises',
            'enterprise_departments',
            'enterprise_employees',
            'enterprise_subscriptions',
            'enterprise_subscription_windows',
            'enterprise_key_assignments',
            'enterprise_weekly_allocations',
            'enterprise_allocation_revisions',
            'enterprise_usage_attributions',
            'enterprise_audit_events'
        ] LOOP
            IF to_regclass(format('public.%I', target_table)) IS NOT NULL THEN
                RAISE EXCEPTION 'SHAN-151 pre-existing enterprise table blocks migration 235: %',
                    target_table;
            END IF;
        END LOOP;
    END IF;
END
$$;
