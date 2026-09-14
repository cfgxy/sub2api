CREATE OR REPLACE FUNCTION reject_enterprise_audit_mutation() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'enterprise audit events are append-only';
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS protect_enterprise_audit_append_only ON enterprise_audit_events;
CREATE TRIGGER protect_enterprise_audit_append_only
BEFORE UPDATE OR DELETE ON enterprise_audit_events
FOR EACH ROW EXECUTE FUNCTION reject_enterprise_audit_mutation();
