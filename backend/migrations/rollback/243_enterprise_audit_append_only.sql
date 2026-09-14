DROP TRIGGER IF EXISTS protect_enterprise_audit_append_only ON enterprise_audit_events;
DROP FUNCTION IF EXISTS reject_enterprise_audit_mutation();
