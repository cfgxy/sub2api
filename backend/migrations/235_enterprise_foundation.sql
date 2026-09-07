CREATE TABLE IF NOT EXISTS enterprises (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    dedicated_upstream_user_id BIGINT NOT NULL UNIQUE REFERENCES users(id) ON DELETE RESTRICT,
    status VARCHAR(20) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS enterprise_employees (
    id BIGSERIAL PRIMARY KEY,
    enterprise_id BIGINT NOT NULL REFERENCES enterprises(id) ON DELETE RESTRICT,
    email VARCHAR(255) NOT NULL,
    normalized_email VARCHAR(255) GENERATED ALWAYS AS (LOWER(BTRIM(email))) STORED,
    status VARCHAR(20) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    disabled_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_enterprise_employees_status_disabled_at CHECK (
        (status = 'active' AND disabled_at IS NULL)
        OR (status = 'disabled' AND disabled_at IS NOT NULL)
    ),
    UNIQUE (enterprise_id, normalized_email),
    UNIQUE (enterprise_id, id)
);

CREATE TABLE IF NOT EXISTS enterprise_subscriptions (
    id BIGSERIAL PRIMARY KEY,
    enterprise_id BIGINT NOT NULL REFERENCES enterprises(id) ON DELETE RESTRICT,
    upstream_user_subscription_id BIGINT NOT NULL REFERENCES user_subscriptions(id) ON DELETE RESTRICT,
    status VARCHAR(20) NOT NULL CHECK (status IN ('scheduled', 'active', 'ended', 'cancelled')),
    effective_window_anchor TIMESTAMPTZ NOT NULL,
    activated_at TIMESTAMPTZ,
    ended_at TIMESTAMPTZ,
    actor_ref TEXT NOT NULL DEFAULT 'system',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_enterprise_subscriptions_status_timestamps CHECK (
        (status = 'scheduled' AND activated_at IS NULL AND ended_at IS NULL)
        OR (status = 'active' AND activated_at IS NOT NULL AND ended_at IS NULL)
        OR (status = 'ended' AND activated_at IS NOT NULL AND ended_at IS NOT NULL)
        OR (status = 'cancelled' AND activated_at IS NULL AND ended_at IS NOT NULL)
    ),
    CONSTRAINT ck_enterprise_subscriptions_timestamp_order CHECK (
        activated_at IS NULL OR ended_at IS NULL OR ended_at >= activated_at
    ),
    CONSTRAINT ck_enterprise_subscriptions_actor_ref_nonempty CHECK (BTRIM(actor_ref) <> ''),
    UNIQUE (enterprise_id, id)
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_enterprise_subscriptions_one_active
    ON enterprise_subscriptions (enterprise_id)
    WHERE status = 'active';
CREATE INDEX IF NOT EXISTS idx_enterprise_subscriptions_effective_anchor
    ON enterprise_subscriptions (enterprise_id, effective_window_anchor DESC);

CREATE TABLE IF NOT EXISTS enterprise_subscription_windows (
    id BIGSERIAL PRIMARY KEY,
    enterprise_id BIGINT NOT NULL,
    subscription_id BIGINT NOT NULL,
    source_anchor TIMESTAMPTZ NOT NULL,
    window_start TIMESTAMPTZ NOT NULL,
    window_end TIMESTAMPTZ NOT NULL,
    allocation_snapshot NUMERIC(20,10) NOT NULL DEFAULT 0 CHECK (allocation_snapshot >= 0),
    observed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (window_end > window_start),
    FOREIGN KEY (enterprise_id, subscription_id)
        REFERENCES enterprise_subscriptions (enterprise_id, id) ON DELETE RESTRICT,
    UNIQUE (enterprise_id, subscription_id, source_anchor, window_start, window_end)
);

CREATE INDEX IF NOT EXISTS idx_enterprise_subscription_windows_anchor
    ON enterprise_subscription_windows (enterprise_id, subscription_id, source_anchor DESC);

CREATE TABLE IF NOT EXISTS enterprise_key_assignments (
    id BIGSERIAL PRIMARY KEY,
    enterprise_id BIGINT NOT NULL REFERENCES enterprises(id) ON DELETE RESTRICT,
    employee_id BIGINT NOT NULL,
    api_key_id BIGINT NOT NULL REFERENCES api_keys(id) ON DELETE RESTRICT,
    status VARCHAR(20) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'revoked')),
    assigned_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at TIMESTAMPTZ,
    actor_ref TEXT NOT NULL DEFAULT 'system',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_enterprise_key_assignments_status_revoked_at CHECK (
        (status = 'active' AND revoked_at IS NULL)
        OR (status = 'revoked' AND revoked_at IS NOT NULL)
    ),
    CONSTRAINT ck_enterprise_key_assignments_actor_ref_nonempty CHECK (BTRIM(actor_ref) <> ''),
    FOREIGN KEY (enterprise_id, employee_id)
        REFERENCES enterprise_employees (enterprise_id, id) ON DELETE RESTRICT
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_enterprise_key_assignments_api_key_history
    ON enterprise_key_assignments (api_key_id);
CREATE UNIQUE INDEX IF NOT EXISTS uq_enterprise_key_assignments_active_employee
    ON enterprise_key_assignments (employee_id)
    WHERE status = 'active';
CREATE INDEX IF NOT EXISTS idx_enterprise_key_assignments_enterprise_employee
    ON enterprise_key_assignments (enterprise_id, employee_id, assigned_at DESC);

CREATE TABLE IF NOT EXISTS enterprise_weekly_allocations (
    id BIGSERIAL PRIMARY KEY,
    enterprise_id BIGINT NOT NULL REFERENCES enterprises(id) ON DELETE RESTRICT,
    subscription_id BIGINT NOT NULL,
    weekly_window_anchor TIMESTAMPTZ NOT NULL,
    employee_id BIGINT NOT NULL,
    amount NUMERIC(20,10) NOT NULL CHECK (amount >= 0),
    version BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    FOREIGN KEY (enterprise_id, subscription_id)
        REFERENCES enterprise_subscriptions (enterprise_id, id) ON DELETE RESTRICT,
    FOREIGN KEY (enterprise_id, employee_id)
        REFERENCES enterprise_employees (enterprise_id, id) ON DELETE RESTRICT,
    UNIQUE (enterprise_id, subscription_id, weekly_window_anchor, employee_id),
    UNIQUE (enterprise_id, id)
);

CREATE INDEX IF NOT EXISTS idx_enterprise_weekly_allocations_window
    ON enterprise_weekly_allocations (enterprise_id, subscription_id, weekly_window_anchor, employee_id);

CREATE TABLE IF NOT EXISTS enterprise_allocation_revisions (
    id BIGSERIAL PRIMARY KEY,
    enterprise_id BIGINT NOT NULL,
    allocation_id BIGINT NOT NULL,
    version BIGINT NOT NULL CHECK (version >= 1),
    previous_amount NUMERIC(20,10) CHECK (previous_amount >= 0),
    new_amount NUMERIC(20,10) NOT NULL CHECK (new_amount >= 0),
    reason TEXT NOT NULL,
    actor_ref TEXT NOT NULL DEFAULT 'system',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    FOREIGN KEY (enterprise_id, allocation_id)
        REFERENCES enterprise_weekly_allocations (enterprise_id, id) ON DELETE RESTRICT,
    CONSTRAINT ck_enterprise_allocation_revisions_reason_nonempty CHECK (BTRIM(reason) <> ''),
    CONSTRAINT ck_enterprise_allocation_revisions_actor_ref_nonempty CHECK (BTRIM(actor_ref) <> ''),
    UNIQUE (allocation_id, version)
);

CREATE INDEX IF NOT EXISTS idx_enterprise_allocation_revisions_timeline
    ON enterprise_allocation_revisions (enterprise_id, allocation_id, version DESC);

CREATE TABLE IF NOT EXISTS enterprise_usage_attributions (
    id BIGSERIAL PRIMARY KEY,
    enterprise_id BIGINT NOT NULL REFERENCES enterprises(id) ON DELETE RESTRICT,
    subscription_id BIGINT NOT NULL,
    employee_id BIGINT,
    usage_log_id BIGINT NOT NULL,
    weekly_window_anchor TIMESTAMPTZ NOT NULL,
    classification VARCHAR(40) NOT NULL CHECK (classification IN ('employee', 'controlled_external')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_enterprise_usage_attributions_classification_employee CHECK (
        (classification = 'employee' AND employee_id IS NOT NULL)
        OR (classification = 'controlled_external' AND employee_id IS NULL)
    ),
    FOREIGN KEY (enterprise_id, subscription_id)
        REFERENCES enterprise_subscriptions (enterprise_id, id) ON DELETE RESTRICT,
    FOREIGN KEY (enterprise_id, employee_id)
        REFERENCES enterprise_employees (enterprise_id, id) ON DELETE RESTRICT,
    UNIQUE (usage_log_id)
);

-- usage_logs is partitioned and removed by retention jobs. Keeping this reference
-- without a foreign key preserves attribution history without blocking retention;
-- actual cost remains authoritative only while the referenced usage_logs row exists.
COMMENT ON COLUMN enterprise_usage_attributions.usage_log_id IS
    'Logical reference only: no FK because usage_logs retention deletes old partitions/rows';

CREATE INDEX IF NOT EXISTS idx_enterprise_usage_attributions_window
    ON enterprise_usage_attributions (enterprise_id, subscription_id, weekly_window_anchor, employee_id);
CREATE INDEX IF NOT EXISTS idx_enterprise_usage_attributions_usage_log
    ON enterprise_usage_attributions (usage_log_id);

CREATE TABLE IF NOT EXISTS enterprise_audit_events (
    id BIGSERIAL PRIMARY KEY,
    enterprise_id BIGINT NOT NULL REFERENCES enterprises(id) ON DELETE RESTRICT,
    event_type VARCHAR(100) NOT NULL,
    entity_type VARCHAR(100) NOT NULL,
    entity_id BIGINT,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    actor_ref TEXT NOT NULL DEFAULT 'system',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_enterprise_audit_events_event_type_nonempty CHECK (BTRIM(event_type) <> ''),
    CONSTRAINT ck_enterprise_audit_events_entity_type_nonempty CHECK (BTRIM(entity_type) <> ''),
    CONSTRAINT ck_enterprise_audit_events_actor_ref_nonempty CHECK (BTRIM(actor_ref) <> '')
);

CREATE INDEX IF NOT EXISTS idx_enterprise_audit_events_timeline
    ON enterprise_audit_events (enterprise_id, created_at DESC, id DESC);

CREATE OR REPLACE FUNCTION validate_enterprise_subscription_owner() RETURNS trigger AS $$
DECLARE
    enterprise_user_id BIGINT;
    subscription_user_id BIGINT;
    subscription_status VARCHAR(20);
    subscription_starts_at TIMESTAMPTZ;
    subscription_weekly_anchor TIMESTAMPTZ;
    subscription_expires_at TIMESTAMPTZ;
    subscription_deleted_at TIMESTAMPTZ;
    next_weekly_anchor TIMESTAMPTZ;
BEGIN
    SELECT dedicated_upstream_user_id INTO enterprise_user_id
    FROM enterprises WHERE id = NEW.enterprise_id;
    SELECT user_id, status, starts_at, weekly_window_start, expires_at, deleted_at
    INTO subscription_user_id, subscription_status, subscription_starts_at,
         subscription_weekly_anchor, subscription_expires_at, subscription_deleted_at
    FROM user_subscriptions WHERE id = NEW.upstream_user_subscription_id;
    IF enterprise_user_id IS DISTINCT FROM subscription_user_id THEN
        RAISE EXCEPTION 'enterprise subscription must belong to its dedicated upstream user';
    END IF;
    IF NEW.status IN ('ended', 'cancelled') AND NEW.ended_at > CURRENT_TIMESTAMP THEN
        RAISE EXCEPTION 'enterprise subscription ended_at cannot be in the future';
    END IF;
    IF NEW.status = 'active' THEN
        IF subscription_deleted_at IS NOT NULL THEN
            RAISE EXCEPTION 'active enterprise subscription requires non-deleted upstream subscription';
        END IF;
        IF subscription_status IS DISTINCT FROM 'active' THEN
            RAISE EXCEPTION 'active enterprise subscription requires active upstream subscription';
        END IF;
        IF subscription_weekly_anchor IS NULL THEN
            RAISE EXCEPTION 'active enterprise subscription requires upstream weekly window anchor';
        END IF;
        IF CURRENT_TIMESTAMP >= subscription_expires_at THEN
            RAISE EXCEPTION 'active enterprise subscription upstream subscription is expired';
        END IF;
        IF CURRENT_TIMESTAMP < subscription_starts_at THEN
            RAISE EXCEPTION 'active enterprise subscription upstream subscription is not currently valid';
        END IF;
        IF NEW.activated_at < subscription_starts_at THEN
            RAISE EXCEPTION 'enterprise subscription activation cannot precede upstream start';
        END IF;
        IF NEW.activated_at > CURRENT_TIMESTAMP THEN
            RAISE EXCEPTION 'enterprise subscription activation cannot be in the future';
        END IF;
        next_weekly_anchor := (
            CASE
                WHEN subscription_weekly_anchor = DATE_TRUNC('day', subscription_starts_at)
                     AND subscription_weekly_anchor < subscription_starts_at
                THEN subscription_starts_at
                ELSE subscription_weekly_anchor
            END
        ) + INTERVAL '7 days';
        IF NEW.effective_window_anchor IS DISTINCT FROM next_weekly_anchor THEN
            RAISE EXCEPTION 'enterprise effective window anchor must match next upstream weekly window';
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER validate_enterprise_subscription_owner
BEFORE INSERT OR UPDATE
ON enterprise_subscriptions
FOR EACH ROW EXECUTE FUNCTION validate_enterprise_subscription_owner();

CREATE OR REPLACE FUNCTION protect_enterprise_subscription_update() RETURNS trigger AS $$
BEGIN
    IF OLD.enterprise_id IS DISTINCT FROM NEW.enterprise_id
       OR OLD.upstream_user_subscription_id IS DISTINCT FROM NEW.upstream_user_subscription_id
       OR OLD.effective_window_anchor IS DISTINCT FROM NEW.effective_window_anchor
       OR OLD.created_at IS DISTINCT FROM NEW.created_at THEN
        RAISE EXCEPTION 'enterprise subscription immutable identity fields cannot change';
    END IF;
    IF OLD.status IN ('ended', 'cancelled') THEN
        RAISE EXCEPTION 'terminal enterprise subscription cannot be modified';
    END IF;
    IF OLD.status = NEW.status THEN
        IF OLD.activated_at IS DISTINCT FROM NEW.activated_at
           OR OLD.ended_at IS DISTINCT FROM NEW.ended_at
           OR OLD.actor_ref IS DISTINCT FROM NEW.actor_ref THEN
            RAISE EXCEPTION 'enterprise subscription lifecycle fields require a status transition';
        END IF;
        RETURN NEW;
    END IF;
    IF NOT (
        (OLD.status = 'scheduled' AND NEW.status IN ('active', 'cancelled'))
        OR (OLD.status = 'active' AND NEW.status = 'ended')
    ) THEN
        RAISE EXCEPTION 'invalid enterprise subscription status transition';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER protect_enterprise_subscription_update
BEFORE UPDATE ON enterprise_subscriptions
FOR EACH ROW EXECUTE FUNCTION protect_enterprise_subscription_update();

CREATE OR REPLACE FUNCTION validate_enterprise_key_owner() RETURNS trigger AS $$
DECLARE
    enterprise_user_id BIGINT;
    key_user_id BIGINT;
BEGIN
    SELECT dedicated_upstream_user_id INTO enterprise_user_id
    FROM enterprises WHERE id = NEW.enterprise_id;
    SELECT user_id INTO key_user_id FROM api_keys WHERE id = NEW.api_key_id;
    IF enterprise_user_id IS DISTINCT FROM key_user_id THEN
        RAISE EXCEPTION 'enterprise key must belong to its dedicated upstream user';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER validate_enterprise_key_owner
BEFORE INSERT OR UPDATE OF enterprise_id, api_key_id
ON enterprise_key_assignments
FOR EACH ROW EXECUTE FUNCTION validate_enterprise_key_owner();

CREATE OR REPLACE FUNCTION validate_enterprise_usage_attribution() RETURNS trigger AS $$
DECLARE
    enterprise_user_id BIGINT;
    upstream_subscription_id BIGINT;
    usage_user_id BIGINT;
    usage_subscription_id BIGINT;
    usage_api_key_id BIGINT;
    usage_created_at TIMESTAMPTZ;
BEGIN
    SELECT dedicated_upstream_user_id INTO enterprise_user_id
    FROM enterprises WHERE id = NEW.enterprise_id;
    SELECT upstream_user_subscription_id INTO upstream_subscription_id
    FROM enterprise_subscriptions
    WHERE id = NEW.subscription_id AND enterprise_id = NEW.enterprise_id;

    SELECT user_id, subscription_id, api_key_id, created_at
    INTO usage_user_id, usage_subscription_id, usage_api_key_id, usage_created_at
    FROM usage_logs WHERE id = NEW.usage_log_id;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'enterprise usage attribution references unknown usage log';
    END IF;
    IF usage_user_id IS DISTINCT FROM enterprise_user_id THEN
        RAISE EXCEPTION 'enterprise usage attribution user mismatch';
    END IF;
    IF usage_subscription_id IS DISTINCT FROM upstream_subscription_id THEN
        RAISE EXCEPTION 'enterprise usage attribution subscription mismatch';
    END IF;
    IF NOT EXISTS (
        SELECT 1
        FROM enterprise_subscription_windows
        WHERE enterprise_id = NEW.enterprise_id
          AND subscription_id = NEW.subscription_id
          AND source_anchor = NEW.weekly_window_anchor
          AND usage_created_at >= window_start
          AND usage_created_at < window_end
    ) THEN
        RAISE EXCEPTION 'enterprise usage attribution does not match subscription window';
    END IF;
    IF NEW.classification = 'employee' AND NOT EXISTS (
        SELECT 1
        FROM enterprise_key_assignments
        WHERE enterprise_id = NEW.enterprise_id
          AND employee_id = NEW.employee_id
          AND api_key_id = usage_api_key_id
          AND assigned_at <= usage_created_at
          AND (revoked_at IS NULL OR usage_created_at <= revoked_at)
    ) THEN
        RAISE EXCEPTION 'enterprise usage attribution employee key mismatch';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER validate_enterprise_usage_attribution
BEFORE INSERT ON enterprise_usage_attributions
FOR EACH ROW EXECUTE FUNCTION validate_enterprise_usage_attribution();

CREATE OR REPLACE FUNCTION reject_enterprise_history_mutation() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION '% is append-only', TG_TABLE_NAME;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER enterprise_subscription_windows_immutable
BEFORE UPDATE OR DELETE ON enterprise_subscription_windows
FOR EACH ROW EXECUTE FUNCTION reject_enterprise_history_mutation();
CREATE TRIGGER enterprise_allocation_revisions_immutable
BEFORE UPDATE OR DELETE ON enterprise_allocation_revisions
FOR EACH ROW EXECUTE FUNCTION reject_enterprise_history_mutation();
CREATE TRIGGER enterprise_usage_attributions_immutable
BEFORE UPDATE OR DELETE ON enterprise_usage_attributions
FOR EACH ROW EXECUTE FUNCTION reject_enterprise_history_mutation();
CREATE TRIGGER enterprise_audit_events_immutable
BEFORE UPDATE OR DELETE ON enterprise_audit_events
FOR EACH ROW EXECUTE FUNCTION reject_enterprise_history_mutation();

CREATE OR REPLACE FUNCTION protect_enterprise_subscription_history() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'enterprise subscriptions cannot be deleted';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER enterprise_subscriptions_no_delete
BEFORE DELETE ON enterprise_subscriptions
FOR EACH ROW EXECUTE FUNCTION protect_enterprise_subscription_history();

CREATE TRIGGER enterprise_employees_no_delete
BEFORE DELETE ON enterprise_employees
FOR EACH ROW EXECUTE FUNCTION reject_enterprise_history_mutation();

CREATE TRIGGER enterprise_weekly_allocations_no_delete
BEFORE DELETE ON enterprise_weekly_allocations
FOR EACH ROW EXECUTE FUNCTION reject_enterprise_history_mutation();

CREATE OR REPLACE FUNCTION protect_enterprise_key_assignment_history() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'enterprise key assignments cannot be deleted';
    END IF;
    IF OLD.status = 'revoked' THEN
        RAISE EXCEPTION 'revoked enterprise key assignments cannot be modified';
    END IF;
    IF OLD.enterprise_id IS DISTINCT FROM NEW.enterprise_id
       OR OLD.employee_id IS DISTINCT FROM NEW.employee_id
       OR OLD.api_key_id IS DISTINCT FROM NEW.api_key_id
       OR OLD.assigned_at IS DISTINCT FROM NEW.assigned_at
       OR OLD.created_at IS DISTINCT FROM NEW.created_at THEN
        RAISE EXCEPTION 'enterprise key assignment identity fields cannot change';
    END IF;
    IF NEW.status IS DISTINCT FROM 'revoked' THEN
        RAISE EXCEPTION 'active enterprise key assignments may only transition to revoked';
    END IF;
    IF NEW.revoked_at IS NULL OR NEW.revoked_at < OLD.assigned_at OR NEW.revoked_at > CURRENT_TIMESTAMP THEN
        RAISE EXCEPTION 'enterprise key revocation time is invalid';
    END IF;
    IF BTRIM(NEW.actor_ref) = '' OR NEW.actor_ref IS NOT DISTINCT FROM OLD.actor_ref THEN
        RAISE EXCEPTION 'enterprise key revocation requires a new actor ref';
    END IF;
    IF NEW.updated_at <= OLD.updated_at THEN
        RAISE EXCEPTION 'enterprise key revocation must advance updated_at';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER enterprise_key_assignments_history
BEFORE UPDATE OR DELETE ON enterprise_key_assignments
FOR EACH ROW EXECUTE FUNCTION protect_enterprise_key_assignment_history();
