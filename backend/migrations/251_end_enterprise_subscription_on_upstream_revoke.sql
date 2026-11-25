-- Keep enterprise state consistent when an administrator revokes its source subscription.
-- A user subscription is soft-deleted; active enterprise bindings must end in the
-- same database transaction so the enterprise portal cannot read stale upstream data.
CREATE OR REPLACE FUNCTION end_enterprise_subscriptions_on_upstream_revoke() RETURNS trigger AS $$
DECLARE
    affected RECORD;
BEGIN
    IF OLD.deleted_at IS NULL AND NEW.deleted_at IS NOT NULL THEN
        FOR affected IN
            UPDATE enterprise_subscriptions
            SET status = 'ended',
                ended_at = NEW.deleted_at,
                updated_at = NEW.deleted_at,
                actor_ref = 'system:upstream_subscription_revoked'
            WHERE upstream_user_subscription_id = NEW.id
              AND status = 'active'
            RETURNING id, enterprise_id
        LOOP
            INSERT INTO enterprise_audit_events (
                enterprise_id, event_type, entity_type, entity_id, payload, actor_ref
            ) VALUES (
                affected.enterprise_id,
                'subscription.source_revoked',
                'enterprise_subscription',
                affected.id,
                jsonb_build_object('upstream_subscription_id', NEW.id),
                'system:upstream_subscription_revoked'
            );
        END LOOP;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_end_enterprise_subscription_on_upstream_revoke ON user_subscriptions;
CREATE TRIGGER trg_end_enterprise_subscription_on_upstream_revoke
AFTER UPDATE OF deleted_at ON user_subscriptions
FOR EACH ROW EXECUTE FUNCTION end_enterprise_subscriptions_on_upstream_revoke();
