-- Inverse of 235_enterprise_foundation.sql.
-- Drops the enterprise tables child-to-parent so foreign keys never block,
-- then removes the 235 trigger functions. Migration 236 already dropped the
-- triggers and functions; IF EXISTS keeps both execution orders safe.

DROP TABLE IF EXISTS enterprise_audit_events;
DROP TABLE IF EXISTS enterprise_usage_attributions;
DROP TABLE IF EXISTS enterprise_allocation_revisions;
DROP TABLE IF EXISTS enterprise_weekly_allocations;
DROP TABLE IF EXISTS enterprise_key_assignments;
DROP TABLE IF EXISTS enterprise_subscription_windows;
DROP TABLE IF EXISTS enterprise_subscriptions;
DROP TABLE IF EXISTS enterprise_employees;
DROP TABLE IF EXISTS enterprises;

DROP FUNCTION IF EXISTS validate_enterprise_subscription_owner();
DROP FUNCTION IF EXISTS protect_enterprise_subscription_update();
DROP FUNCTION IF EXISTS validate_enterprise_key_owner();
DROP FUNCTION IF EXISTS validate_enterprise_usage_attribution();
DROP FUNCTION IF EXISTS reject_enterprise_history_mutation();
DROP FUNCTION IF EXISTS protect_enterprise_subscription_history();
DROP FUNCTION IF EXISTS protect_enterprise_key_assignment_history();
