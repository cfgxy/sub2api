package enterprise

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrAllocationVersionConflict  = errors.New("enterprise allocation version conflict")
	ErrActorRefRequired           = errors.New("enterprise actor ref is required")
	ErrReasonRequired             = errors.New("enterprise allocation reason is required")
	ErrEnterpriseOwnership        = errors.New("enterprise upstream ownership mismatch")
	ErrObservedWindowConflict     = errors.New("enterprise observed window conflict")
	ErrObservedWindowMismatch     = errors.New("enterprise observed window does not match upstream")
	ErrKeyGenerationRevoked       = errors.New("enterprise api key generation was revoked")
	ErrKeyAlreadyAssigned         = errors.New("enterprise api key is assigned to another employee")
	ErrUsageAttributionMismatch   = errors.New("enterprise usage attribution snapshot mismatch")
	ErrInvalidWindowType          = errors.New("invalid enterprise allocation window type")
	ErrInvalidWindowAnchor        = errors.New("invalid enterprise allocation window anchor")
	ErrEnterpriseAccessDenied     = errors.New("enterprise allocation access denied")
	ErrAllocationLimitUnavailable = errors.New("enterprise allocation limit is unavailable")
	ErrAllocationLimitExceeded    = errors.New("enterprise allocation limit exceeded")
)

const (
	WindowType5h = "5h"
	WindowType7d = "7d"
)

type Repository struct {
	db                   *sql.DB
	authCacheInvalidator AuthCacheInvalidator
}

type AuthCacheInvalidator interface {
	InvalidateAuthCacheByKey(ctx context.Context, key string)
}

type Allocation struct {
	ID                 int64     `json:"id"`
	EnterpriseID       int64     `json:"enterprise_id"`
	SubscriptionID     int64     `json:"subscription_id"`
	WeeklyWindowAnchor time.Time `json:"-"`
	WindowType         string    `json:"window_type"`
	WindowAnchor       time.Time `json:"window_anchor"`
	EmployeeID         int64     `json:"employee_id"`
	Amount             string    `json:"amount"`
	Version            int64     `json:"version"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type CreateAllocationParams struct {
	EnterpriseID       int64
	SubscriptionID     int64
	WeeklyWindowAnchor time.Time
	EmployeeID         int64
	Amount             string
	Reason             string
	ActorRef           string
}

type ReviseAllocationParams struct {
	AllocationID    int64
	ExpectedVersion int64
	Amount          string
	Reason          string
	ActorRef        string
}

type AllocationUsageSummaryQuery struct {
	RequesterUserID    int64
	EnterpriseID       int64
	SubscriptionID     int64
	WeeklyWindowAnchor time.Time
	EmployeeID         int64
	WindowType         string
	WindowAnchor       time.Time
}

type SetAllocationParams struct {
	RequesterUserID int64
	EnterpriseID    int64
	SubscriptionID  int64
	EmployeeID      int64
	WindowType      string
	WindowAnchor    time.Time
	Amount          string
	ExpectedVersion int64
	Reason          string
}

type SetAllocationLimitsParams struct {
	RequesterUserID int64
	EnterpriseID    int64
	SubscriptionID  int64
	WindowAnchor    time.Time
	Limit5h         string
	Limit7d         string
	Reason          string
}

type AllocationUsageSummary struct {
	Allocation string `json:"allocation"`
	ActualCost string `json:"actual_cost"`
	Remaining  string `json:"remaining"`
	Overage    string `json:"overage"`
}

type ReplaceScheduledSubscriptionParams struct {
	EnterpriseID           int64
	UpstreamSubscriptionID int64
	ActorRef               string
}

type ScheduledSubscriptionReplacement struct {
	ScheduledSubscriptionID int64
	CancelledSubscriptionID *int64
}

type ObserveWeeklyWindowParams struct {
	EnterpriseID           int64
	UpstreamSubscriptionID int64
	ExpectedWindowStart    *time.Time
	ObservedWindowStart    *time.Time
	ActorRef               string
}

type ObservedWeeklyWindowResult struct {
	Advanced           bool
	CurrentWindowStart *time.Time
}

type RebindKeyAssignmentParams struct {
	EnterpriseID           int64
	EmployeeID             int64
	APIKeyID               int64
	UpstreamSubscriptionID int64
	ActorRef               string
}

type KeyAssignment struct {
	ID                     int64
	EnterpriseID           int64
	EmployeeID             int64
	APIKeyID               int64
	UpstreamSubscriptionID int64
	UpstreamGroupID        int64
	Generation             int64
	AssignedAt             time.Time
}

type RevokeKeyGenerationParams struct {
	EnterpriseID int64
	APIKeyID     int64
	ActorRef     string
}

type CreateUsageAttributionParams struct {
	EnterpriseID       int64
	SubscriptionID     int64
	EmployeeID         *int64
	UsageLogID         int64
	WeeklyWindowAnchor time.Time
	Classification     string
}

type CreateUsageAttributionSnapshotParams struct {
	RequesterUserID        int64
	EnterpriseID           int64
	SubscriptionID         int64
	EmployeeID             *int64
	APIKeyID               int64
	UsageLogID             int64
	UpstreamSubscriptionID int64
	RequestAt              time.Time
	Classification         string
}

type UsageAttribution struct {
	ID                   int64
	EnterpriseID         int64
	SubscriptionID       int64
	EmployeeID           *int64
	APIKeyID             int64
	UsageLogID           int64
	AssignmentGeneration int64
	WindowType           string
	WindowAnchor         time.Time
	WeeklyWindowAnchor   time.Time
	RequestAt            time.Time
	Classification       string
	CreatedAt            time.Time
}

type AllocationRevision struct {
	ID             int64
	AllocationID   int64
	Version        int64
	PreviousAmount *string
	NewAmount      string
	Reason         string
	ActorRef       string
	CreatedAt      time.Time
}

type AuditEvent struct {
	ID         int64
	EventType  string
	EntityType string
	EntityID   *int64
	Payload    json.RawMessage
	ActorRef   string
	CreatedAt  time.Time
}

func NewRepository(db *sql.DB, authCacheInvalidator AuthCacheInvalidator) *Repository {
	if authCacheInvalidator == nil {
		panic("enterprise auth cache invalidator is required")
	}
	return &Repository{db: db, authCacheInvalidator: authCacheInvalidator}
}

func ValidateAllocationWindow(windowType string, windowAnchor time.Time) error {
	if windowType != WindowType5h && windowType != WindowType7d {
		return ErrInvalidWindowType
	}
	if windowAnchor.IsZero() {
		return ErrInvalidWindowAnchor
	}
	return nil
}

func (r *Repository) SetAllocationLimits(ctx context.Context, params SetAllocationLimitsParams) error {
	reason, err := NormalizeAllocationReason(params.Reason)
	if err != nil {
		return err
	}
	limit5h, err := NormalizeAmount(params.Limit5h)
	if err != nil {
		return err
	}
	limit7d, err := NormalizeAmount(params.Limit7d)
	if err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var windowID int64
	if err = tx.QueryRowContext(ctx, `
		UPDATE enterprise_subscription_windows AS subscription_window
		SET allocation_limit_5h = $1::numeric,
		    allocation_limit_7d = $2::numeric,
		    allocation_limit_reason = $3,
		    allocation_limit_actor_ref = $4
		FROM enterprise_subscriptions AS enterprise_subscription
		JOIN enterprises AS enterprise ON enterprise.id = enterprise_subscription.enterprise_id
		WHERE subscription_window.enterprise_id = $5
		  AND subscription_window.subscription_id = $6
		  AND subscription_window.window_start = $7
		  AND enterprise_subscription.id = subscription_window.subscription_id
		  AND enterprise_subscription.enterprise_id = subscription_window.enterprise_id
		  AND enterprise.dedicated_upstream_user_id = $8
		RETURNING subscription_window.id
	`, limit5h, limit7d, reason, fmt.Sprintf("user:%d", params.RequesterUserID),
		params.EnterpriseID, params.SubscriptionID, params.WindowAnchor.UTC(), params.RequesterUserID).Scan(&windowID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrEnterpriseAccessDenied
		}
		return err
	}
	payload, err := json.Marshal(map[string]any{
		"window_anchor": params.WindowAnchor.UTC(),
		"limit_5h":      limit5h,
		"limit_7d":      limit7d,
		"reason":        reason,
	})
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO enterprise_audit_events (
			enterprise_id, event_type, entity_type, entity_id, payload, actor_ref
		) VALUES ($1, 'allocation.limits_changed', 'enterprise_subscription_window', $2, $3::jsonb, $4)
	`, params.EnterpriseID, windowID, payload, fmt.Sprintf("user:%d", params.RequesterUserID)); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) SetAllocation(ctx context.Context, params SetAllocationParams) (*Allocation, error) {
	var err error
	params.Reason, err = NormalizeAllocationReason(params.Reason)
	if err != nil {
		return nil, err
	}
	if err := ValidateAllocationWindow(params.WindowType, params.WindowAnchor); err != nil {
		return nil, err
	}
	amount, err := NormalizeAmount(params.Amount)
	if err != nil {
		return nil, err
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var limit string
	var canonicalAnchor time.Time
	err = tx.QueryRowContext(ctx, `
		SELECT CASE $1::text
			WHEN '5h' THEN subscription_window.allocation_limit_5h::text
			WHEN '7d' THEN subscription_window.allocation_limit_7d::text
		END,
		CASE $1::text
			WHEN '5h' THEN subscription_window.window_start
				+ FLOOR(EXTRACT(EPOCH FROM (CURRENT_TIMESTAMP - subscription_window.window_start)) / 18000)
				  * INTERVAL '5 hours'
			WHEN '7d' THEN subscription_window.window_start
		END
		FROM enterprise_subscriptions AS enterprise_subscription
		JOIN enterprises AS enterprise
		  ON enterprise.id = enterprise_subscription.enterprise_id
		JOIN user_subscriptions AS upstream_subscription
		  ON upstream_subscription.id = enterprise_subscription.upstream_user_subscription_id
		 AND upstream_subscription.user_id = enterprise.dedicated_upstream_user_id
		 AND upstream_subscription.deleted_at IS NULL
		JOIN enterprise_subscription_windows AS subscription_window
		  ON subscription_window.enterprise_id = enterprise_subscription.enterprise_id
		 AND subscription_window.subscription_id = enterprise_subscription.id
		 AND subscription_window.observed_weekly_window_start = enterprise_subscription.observed_weekly_window_start
		 AND CURRENT_TIMESTAMP >= subscription_window.window_start
		 AND CURRENT_TIMESTAMP < subscription_window.window_end
		JOIN enterprise_employees AS employee
		  ON employee.enterprise_id = enterprise_subscription.enterprise_id
		 AND employee.id = $5
		 AND employee.status = 'active'
		WHERE enterprise_subscription.enterprise_id = $2
		  AND enterprise_subscription.id = $3
		  AND enterprise.dedicated_upstream_user_id = $4
		  AND enterprise.status = 'active'
		FOR UPDATE OF enterprise_subscription, subscription_window
	`, params.WindowType, params.EnterpriseID, params.SubscriptionID,
		params.RequesterUserID, params.EmployeeID).Scan(&limit, &canonicalAnchor)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrEnterpriseAccessDenied
	}
	if err != nil {
		return nil, err
	}
	canonicalAnchor = canonicalAnchor.UTC()
	if !params.WindowAnchor.UTC().Equal(canonicalAnchor) {
		return nil, ErrInvalidWindowAnchor
	}

	var allowed bool
	if err = tx.QueryRowContext(ctx, `
		WITH totals AS (
			SELECT COALESCE(SUM(amount), 0) AS current_total,
			       COALESCE(SUM(amount) FILTER (WHERE employee_id <> $7), 0) + $2::numeric AS proposed_total
			FROM enterprise_weekly_allocations
			WHERE enterprise_id = $3
			  AND subscription_id = $4
			  AND window_type = $5
			  AND window_anchor = $6
		)
		SELECT $1::numeric > 0 AND (
			proposed_total <= $1::numeric
			OR (current_total > $1::numeric AND proposed_total < current_total)
		)
		FROM totals
	`, limit, amount, params.EnterpriseID, params.SubscriptionID,
		params.WindowType, canonicalAnchor, params.EmployeeID).Scan(&allowed); err != nil {
		return nil, err
	}
	if !allowed {
		return nil, ErrAllocationLimitExceeded
	}

	allocation := &Allocation{}
	var previousAmount sql.NullString
	err = tx.QueryRowContext(ctx, `
		SELECT id, amount::text, version, created_at, updated_at
		FROM enterprise_weekly_allocations
		WHERE enterprise_id = $1 AND subscription_id = $2 AND employee_id = $3
		  AND window_type = $4 AND window_anchor = $5
		FOR UPDATE
	`, params.EnterpriseID, params.SubscriptionID, params.EmployeeID,
		params.WindowType, canonicalAnchor).Scan(
		&allocation.ID, &previousAmount, &allocation.Version, &allocation.CreatedAt, &allocation.UpdatedAt,
	)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if params.ExpectedVersion != 0 {
			return nil, ErrAllocationVersionConflict
		}
		err = tx.QueryRowContext(ctx, `
			INSERT INTO enterprise_weekly_allocations (
				enterprise_id, subscription_id, employee_id, window_type, window_anchor, amount, version
			) VALUES ($1, $2, $3, $4, $5, $6::numeric, 1)
			RETURNING id, version, created_at, updated_at
		`, params.EnterpriseID, params.SubscriptionID, params.EmployeeID, params.WindowType,
			canonicalAnchor, amount).Scan(
			&allocation.ID, &allocation.Version, &allocation.CreatedAt, &allocation.UpdatedAt,
		)
	case err != nil:
		return nil, err
	case allocation.Version != params.ExpectedVersion:
		return nil, ErrAllocationVersionConflict
	default:
		err = tx.QueryRowContext(ctx, `
			UPDATE enterprise_weekly_allocations
			SET amount = $1::numeric, version = version + 1, updated_at = NOW()
			WHERE id = $2 AND version = $3
			RETURNING version, updated_at
		`, amount, allocation.ID, params.ExpectedVersion).Scan(&allocation.Version, &allocation.UpdatedAt)
	}
	if err != nil {
		return nil, err
	}

	allocation.EnterpriseID = params.EnterpriseID
	allocation.SubscriptionID = params.SubscriptionID
	allocation.EmployeeID = params.EmployeeID
	allocation.WindowType = params.WindowType
	allocation.WindowAnchor = canonicalAnchor
	allocation.WeeklyWindowAnchor = allocation.WindowAnchor
	allocation.Amount = amount
	actorRef := fmt.Sprintf("user:%d", params.RequesterUserID)
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO enterprise_allocation_revisions (
			enterprise_id, allocation_id, version, previous_amount, new_amount, reason, actor_ref
		) VALUES ($1, $2, $3, $4::numeric, $5::numeric, $6, $7)
	`, allocation.EnterpriseID, allocation.ID, allocation.Version, previousAmount,
		allocation.Amount, params.Reason, actorRef); err != nil {
		return nil, err
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return allocation, nil
}

func (r *Repository) CreateAllocation(ctx context.Context, params CreateAllocationParams) (*Allocation, error) {
	params.ActorRef = strings.TrimSpace(params.ActorRef)
	if params.ActorRef == "" {
		return nil, ErrActorRefRequired
	}
	params.Reason = strings.TrimSpace(params.Reason)
	if params.Reason == "" {
		return nil, ErrReasonRequired
	}
	amount, err := NormalizeAmount(params.Amount)
	if err != nil {
		return nil, err
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	allocation := &Allocation{}
	err = tx.QueryRowContext(ctx, `
			INSERT INTO enterprise_weekly_allocations (
				enterprise_id, subscription_id, window_type, window_anchor, employee_id, amount, version
			) VALUES ($1, $2, '7d', $3, $4, $5::numeric, 1)
			RETURNING id, enterprise_id, subscription_id, window_anchor, employee_id,
		          amount::text, version, created_at, updated_at
	`, params.EnterpriseID, params.SubscriptionID, params.WeeklyWindowAnchor, params.EmployeeID, amount).Scan(
		&allocation.ID,
		&allocation.EnterpriseID,
		&allocation.SubscriptionID,
		&allocation.WeeklyWindowAnchor,
		&allocation.EmployeeID,
		&allocation.Amount,
		&allocation.Version,
		&allocation.CreatedAt,
		&allocation.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	if _, err = tx.ExecContext(ctx, `
		INSERT INTO enterprise_allocation_revisions (
			enterprise_id, allocation_id, version, previous_amount, new_amount, reason, actor_ref
		) VALUES ($1, $2, $3, NULL, $4::numeric, $5, $6)
	`, allocation.EnterpriseID, allocation.ID, allocation.Version, allocation.Amount, params.Reason, params.ActorRef); err != nil {
		return nil, err
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return allocation, nil
}

func (r *Repository) ReviseAllocation(ctx context.Context, params ReviseAllocationParams) (*Allocation, error) {
	params.ActorRef = strings.TrimSpace(params.ActorRef)
	if params.ActorRef == "" {
		return nil, ErrActorRefRequired
	}
	params.Reason = strings.TrimSpace(params.Reason)
	if params.Reason == "" {
		return nil, ErrReasonRequired
	}
	amount, err := NormalizeAmount(params.Amount)
	if err != nil {
		return nil, err
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	allocation := &Allocation{}
	var previousAmount string
	err = tx.QueryRowContext(ctx, `
			SELECT id, enterprise_id, subscription_id, window_anchor, employee_id,
		       amount::text, version, created_at, updated_at
		FROM enterprise_weekly_allocations
		WHERE id = $1
		FOR UPDATE
	`, params.AllocationID).Scan(
		&allocation.ID,
		&allocation.EnterpriseID,
		&allocation.SubscriptionID,
		&allocation.WeeklyWindowAnchor,
		&allocation.EmployeeID,
		&previousAmount,
		&allocation.Version,
		&allocation.CreatedAt,
		&allocation.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAllocationVersionConflict
	}
	if err != nil {
		return nil, err
	}
	if allocation.Version != params.ExpectedVersion {
		return nil, ErrAllocationVersionConflict
	}

	err = tx.QueryRowContext(ctx, `
		UPDATE enterprise_weekly_allocations
		SET amount = $1::numeric,
		    version = version + 1,
		    updated_at = NOW()
		WHERE id = $2 AND version = $3
			RETURNING id, enterprise_id, subscription_id, window_anchor, employee_id,
		          amount::text, version, created_at, updated_at
	`, amount, params.AllocationID, params.ExpectedVersion).Scan(
		&allocation.ID,
		&allocation.EnterpriseID,
		&allocation.SubscriptionID,
		&allocation.WeeklyWindowAnchor,
		&allocation.EmployeeID,
		&allocation.Amount,
		&allocation.Version,
		&allocation.CreatedAt,
		&allocation.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAllocationVersionConflict
	}
	if err != nil {
		return nil, err
	}

	if _, err = tx.ExecContext(ctx, `
		INSERT INTO enterprise_allocation_revisions (
			enterprise_id, allocation_id, version, previous_amount, new_amount, reason, actor_ref
		) VALUES ($1, $2, $3, $4::numeric, $5::numeric, $6, $7)
	`, allocation.EnterpriseID, allocation.ID, allocation.Version, previousAmount, allocation.Amount, params.Reason, params.ActorRef); err != nil {
		return nil, err
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return allocation, nil
}

func (r *Repository) GetAllocationUsageSummary(ctx context.Context, query AllocationUsageSummaryQuery) (*AllocationUsageSummary, error) {
	if query.WindowType == "" {
		query.WindowType = WindowType7d
		query.WindowAnchor = query.WeeklyWindowAnchor
	}
	if err := ValidateAllocationWindow(query.WindowType, query.WindowAnchor); err != nil {
		return nil, err
	}
	summary := &AllocationUsageSummary{}
	err := r.db.QueryRowContext(ctx, `
		WITH usage_total AS (
			SELECT COALESCE(SUM(usage_log.actual_cost), 0)::NUMERIC(20,8) AS actual_cost
			FROM enterprise_usage_attributions AS attribution
			JOIN enterprise_subscriptions AS subscription
			  ON subscription.enterprise_id = attribution.enterprise_id
			 AND subscription.id = attribution.subscription_id
			JOIN enterprises AS enterprise
			  ON enterprise.id = attribution.enterprise_id
			JOIN usage_logs AS usage_log
			  ON usage_log.id = attribution.usage_log_id
			 AND usage_log.api_key_id = attribution.api_key_id
			 AND usage_log.user_id = enterprise.dedicated_upstream_user_id
			 AND usage_log.subscription_id = subscription.upstream_user_subscription_id
			WHERE attribution.enterprise_id = $1
			  AND attribution.subscription_id = $2
			  AND attribution.window_anchor = $3
			  AND attribution.employee_id = $4
			  AND attribution.window_type = $5
			  AND attribution.classification = 'employee'
		)
		SELECT allocation.amount::text,
		       usage_total.actual_cost::text,
		       GREATEST(allocation.amount - usage_total.actual_cost, 0)::NUMERIC(20,8)::text,
		       GREATEST(usage_total.actual_cost - allocation.amount, 0)::NUMERIC(20,8)::text
		FROM enterprise_weekly_allocations AS allocation
		CROSS JOIN usage_total
		WHERE allocation.enterprise_id = $1
		  AND allocation.subscription_id = $2
		  AND allocation.window_anchor = $3
		  AND allocation.employee_id = $4
		  AND allocation.window_type = $5
		  AND ($6 = 0 OR EXISTS (
				SELECT 1 FROM enterprises
				WHERE id = $1 AND dedicated_upstream_user_id = $6 AND status = 'active'
		  ))
	`, query.EnterpriseID, query.SubscriptionID, query.WindowAnchor.UTC(), query.EmployeeID,
		query.WindowType, query.RequesterUserID).Scan(
		&summary.Allocation,
		&summary.ActualCost,
		&summary.Remaining,
		&summary.Overage,
	)
	if err != nil {
		return nil, err
	}
	return summary, nil
}

func (r *Repository) ReplaceScheduledSubscription(
	ctx context.Context,
	params ReplaceScheduledSubscriptionParams,
) (*ScheduledSubscriptionReplacement, error) {
	params.ActorRef = strings.TrimSpace(params.ActorRef)
	if params.ActorRef == "" {
		return nil, ErrActorRefRequired
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var enterpriseUserID int64
	if err = tx.QueryRowContext(ctx, `
		SELECT dedicated_upstream_user_id
		FROM enterprises
		WHERE id = $1
		FOR UPDATE
	`, params.EnterpriseID).Scan(&enterpriseUserID); err != nil {
		return nil, err
	}

	var subscriptionUserID int64
	if err = tx.QueryRowContext(ctx, `
		SELECT user_id
		FROM user_subscriptions
		WHERE id = $1 AND deleted_at IS NULL
	`, params.UpstreamSubscriptionID).Scan(&subscriptionUserID); err != nil {
		return nil, err
	}
	if subscriptionUserID != enterpriseUserID {
		return nil, ErrEnterpriseOwnership
	}

	replacement := &ScheduledSubscriptionReplacement{}
	var cancelledID int64
	err = tx.QueryRowContext(ctx, `
		UPDATE enterprise_subscriptions
		SET status = 'cancelled', ended_at = NOW(), actor_ref = $2, updated_at = NOW()
		WHERE enterprise_id = $1 AND status = 'scheduled'
		RETURNING id
	`, params.EnterpriseID, params.ActorRef).Scan(&cancelledID)
	switch {
	case err == nil:
		replacement.CancelledSubscriptionID = &cancelledID
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return nil, err
	}

	if err = tx.QueryRowContext(ctx, `
		INSERT INTO enterprise_subscriptions (
			enterprise_id, upstream_user_subscription_id, status,
			observed_weekly_window_start, actor_ref
		) VALUES ($1, $2, 'scheduled', NULL, $3)
		RETURNING id
	`, params.EnterpriseID, params.UpstreamSubscriptionID, params.ActorRef).Scan(
		&replacement.ScheduledSubscriptionID,
	); err != nil {
		return nil, err
	}

	payload, err := json.Marshal(map[string]any{
		"cancelled_subscription_id": replacement.CancelledSubscriptionID,
		"scheduled_subscription_id": replacement.ScheduledSubscriptionID,
		"upstream_subscription_id":  params.UpstreamSubscriptionID,
	})
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO enterprise_audit_events (
			enterprise_id, event_type, entity_type, entity_id, payload, actor_ref
		) VALUES ($1, 'subscription.scheduled_replaced', 'enterprise_subscription', $2, $3::jsonb, $4)
	`, params.EnterpriseID, replacement.ScheduledSubscriptionID, payload, params.ActorRef); err != nil {
		return nil, err
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return replacement, nil
}

func (r *Repository) ObserveWeeklyWindow(
	ctx context.Context,
	params ObserveWeeklyWindowParams,
) (*ObservedWeeklyWindowResult, error) {
	params.ActorRef = strings.TrimSpace(params.ActorRef)
	if params.ActorRef == "" {
		return nil, ErrActorRefRequired
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var subscriptionID int64
	var currentWindow, upstreamWindow sql.NullTime
	if err = tx.QueryRowContext(ctx, `
		SELECT enterprise_subscription.id,
		       enterprise_subscription.observed_weekly_window_start,
		       upstream_subscription.weekly_window_start
		FROM enterprise_subscriptions AS enterprise_subscription
		JOIN user_subscriptions AS upstream_subscription
		  ON upstream_subscription.id = enterprise_subscription.upstream_user_subscription_id
		WHERE enterprise_subscription.enterprise_id = $1
		  AND enterprise_subscription.upstream_user_subscription_id = $2
		  AND enterprise_subscription.status = 'active'
		FOR UPDATE OF enterprise_subscription, upstream_subscription
	`, params.EnterpriseID, params.UpstreamSubscriptionID).Scan(
		&subscriptionID,
		&currentWindow,
		&upstreamWindow,
	); err != nil {
		return nil, err
	}

	result := &ObservedWeeklyWindowResult{CurrentWindowStart: nullTimePointer(currentWindow)}
	if params.ObservedWindowStart == nil {
		return result, tx.Commit()
	}
	observed := params.ObservedWindowStart.UTC()
	if currentWindow.Valid && !observed.After(currentWindow.Time) {
		return result, tx.Commit()
	}
	if !upstreamWindow.Valid || !observed.Equal(upstreamWindow.Time) {
		return nil, ErrObservedWindowMismatch
	}
	if !sameNullableTime(params.ExpectedWindowStart, currentWindow) {
		return nil, ErrObservedWindowConflict
	}

	if _, err = tx.ExecContext(ctx, `
		UPDATE enterprise_subscriptions
		SET observed_weekly_window_start = $2, updated_at = NOW(), actor_ref = $3
		WHERE id = $1
	`, subscriptionID, observed, params.ActorRef); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO enterprise_subscription_windows (
			enterprise_id, subscription_id, upstream_user_subscription_id,
			observed_weekly_window_start, window_start, window_end
		) VALUES ($1, $2, $3, $4::timestamptz, $4::timestamptz, $4::timestamptz + INTERVAL '7 days')
		ON CONFLICT (upstream_user_subscription_id, observed_weekly_window_start) DO NOTHING
	`, params.EnterpriseID, subscriptionID, params.UpstreamSubscriptionID, observed); err != nil {
		return nil, err
	}
	payload, err := json.Marshal(map[string]any{
		"upstream_subscription_id": params.UpstreamSubscriptionID,
		"previous_window_start":    result.CurrentWindowStart,
		"observed_window_start":    observed,
	})
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO enterprise_audit_events (
			enterprise_id, event_type, entity_type, entity_id, payload, actor_ref
		) VALUES ($1, 'subscription.weekly_window_observed', 'enterprise_subscription', $2, $3::jsonb, $4)
	`, params.EnterpriseID, subscriptionID, payload, params.ActorRef); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	result.Advanced = true
	result.CurrentWindowStart = &observed
	return result, nil
}

func (r *Repository) RebindKeyAssignment(
	ctx context.Context,
	params RebindKeyAssignmentParams,
) (*KeyAssignment, error) {
	params.ActorRef = strings.TrimSpace(params.ActorRef)
	if params.ActorRef == "" {
		return nil, ErrActorRefRequired
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var enterpriseUserID, keyUserID, upstreamUserID, upstreamGroupID int64
	var keyGroupID sql.NullInt64
	var keyStatus, apiKey string
	if err = tx.QueryRowContext(ctx, `
		SELECT user_id, group_id, status, key
		FROM api_keys
		WHERE id = $1 AND deleted_at IS NULL
		FOR UPDATE
	`, params.APIKeyID).Scan(&keyUserID, &keyGroupID, &keyStatus, &apiKey); err != nil {
		return nil, err
	}
	if err = tx.QueryRowContext(ctx, `
		SELECT dedicated_upstream_user_id
		FROM enterprises
		WHERE id = $1
		FOR UPDATE
	`, params.EnterpriseID).Scan(&enterpriseUserID); err != nil {
		return nil, err
	}
	if err = tx.QueryRowContext(ctx, `
		SELECT user_id, group_id
		FROM user_subscriptions
		WHERE id = $1 AND deleted_at IS NULL
		FOR UPDATE
	`, params.UpstreamSubscriptionID).Scan(&upstreamUserID, &upstreamGroupID); err != nil {
		return nil, err
	}
	if keyUserID != enterpriseUserID || upstreamUserID != enterpriseUserID {
		return nil, ErrEnterpriseOwnership
	}
	if keyStatus != "active" {
		return nil, ErrKeyGenerationRevoked
	}

	var wasRevoked bool
	if err = tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM enterprise_key_assignments
			WHERE api_key_id = $1 AND status = 'revoked'
		)
	`, params.APIKeyID).Scan(&wasRevoked); err != nil {
		return nil, err
	}
	if wasRevoked {
		return nil, ErrKeyGenerationRevoked
	}

	groupChanged := !keyGroupID.Valid || keyGroupID.Int64 != upstreamGroupID
	if groupChanged {
		if _, err = tx.ExecContext(ctx, `
			UPDATE api_keys
			SET group_id = $2, updated_at = clock_timestamp()
			WHERE id = $1
		`, params.APIKeyID, upstreamGroupID); err != nil {
			return nil, err
		}
	}

	var segmentBoundary time.Time
	if err = tx.QueryRowContext(ctx, "SELECT clock_timestamp()").Scan(&segmentBoundary); err != nil {
		return nil, err
	}
	var overlapsExternalAttribution bool
	if err = tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM enterprise_usage_attributions AS attribution
			JOIN usage_logs AS usage_log ON usage_log.id = attribution.usage_log_id
			WHERE attribution.enterprise_id = $1
			  AND attribution.classification = 'controlled_external'
			  AND usage_log.api_key_id = $2
			  AND usage_log.created_at >= $3
		)
	`, params.EnterpriseID, params.APIKeyID, segmentBoundary).
		Scan(&overlapsExternalAttribution); err != nil {
		return nil, err
	}
	if overlapsExternalAttribution {
		return nil, ErrUsageAttributionMismatch
	}

	var activeAssignment KeyAssignment
	err = tx.QueryRowContext(ctx, `
		SELECT id, enterprise_id, employee_id, api_key_id,
		       upstream_user_subscription_id, upstream_group_id, generation, assigned_at
		FROM enterprise_key_assignments
		WHERE api_key_id = $1 AND status = 'active'
		FOR UPDATE
	`, params.APIKeyID).Scan(
		&activeAssignment.ID,
		&activeAssignment.EnterpriseID,
		&activeAssignment.EmployeeID,
		&activeAssignment.APIKeyID,
		&activeAssignment.UpstreamSubscriptionID,
		&activeAssignment.UpstreamGroupID,
		&activeAssignment.Generation,
		&activeAssignment.AssignedAt,
	)
	switch {
	case err == nil && activeAssignment.EmployeeID != params.EmployeeID:
		return nil, ErrKeyAlreadyAssigned
	case err == nil && activeAssignment.UpstreamSubscriptionID == params.UpstreamSubscriptionID &&
		activeAssignment.UpstreamGroupID == upstreamGroupID:
		if err = tx.Commit(); err != nil {
			return nil, err
		}
		if groupChanged {
			r.authCacheInvalidator.InvalidateAuthCacheByKey(ctx, apiKey)
		}
		return &activeAssignment, nil
	case err == nil:
		if _, err = tx.ExecContext(ctx, `
			UPDATE enterprise_key_assignments
			SET status = 'ended', ended_at = $3, actor_ref = $2, updated_at = $3
			WHERE id = $1
		`, activeAssignment.ID, params.ActorRef, segmentBoundary); err != nil {
			return nil, err
		}
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return nil, err
	}

	assignment := &KeyAssignment{}
	if err = tx.QueryRowContext(ctx, `
		INSERT INTO enterprise_key_assignments (
			enterprise_id, employee_id, api_key_id,
			upstream_user_subscription_id, upstream_group_id,
			generation, status, actor_ref, assigned_at
		) VALUES (
			$1, $2, $3, $4, $5,
			COALESCE((SELECT MAX(generation) FROM enterprise_key_assignments WHERE api_key_id = $3), 1),
			'active', $6, $7
		)
		RETURNING id, enterprise_id, employee_id, api_key_id,
		          upstream_user_subscription_id, upstream_group_id, generation, assigned_at
	`, params.EnterpriseID, params.EmployeeID, params.APIKeyID,
		params.UpstreamSubscriptionID, upstreamGroupID, params.ActorRef, segmentBoundary).Scan(
		&assignment.ID,
		&assignment.EnterpriseID,
		&assignment.EmployeeID,
		&assignment.APIKeyID,
		&assignment.UpstreamSubscriptionID,
		&assignment.UpstreamGroupID,
		&assignment.Generation,
		&assignment.AssignedAt,
	); err != nil {
		return nil, err
	}
	payload, err := json.Marshal(map[string]any{
		"api_key_id":                  params.APIKeyID,
		"previous_assignment_id":      activeAssignment.ID,
		"new_assignment_id":           assignment.ID,
		"upstream_subscription_id":    params.UpstreamSubscriptionID,
		"upstream_group_id":           upstreamGroupID,
		"assignment_segment_boundary": segmentBoundary,
	})
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO enterprise_audit_events (
			enterprise_id, event_type, entity_type, entity_id, payload, actor_ref
		) VALUES ($1, 'key.assignment_segment_rebound', 'enterprise_key_assignment', $2, $3::jsonb, $4)
	`, params.EnterpriseID, assignment.ID, payload, params.ActorRef); err != nil {
		return nil, err
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}
	if groupChanged {
		r.authCacheInvalidator.InvalidateAuthCacheByKey(ctx, apiKey)
	}
	return assignment, nil
}

func (r *Repository) RevokeKeyGeneration(ctx context.Context, params RevokeKeyGenerationParams) error {
	params.ActorRef = strings.TrimSpace(params.ActorRef)
	if params.ActorRef == "" {
		return ErrActorRefRequired
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var keyStatus, apiKey string
	var keyUserID, enterpriseUserID int64
	if err = tx.QueryRowContext(ctx, `
			SELECT api_key.status, api_key.key, api_key.user_id, enterprise.dedicated_upstream_user_id
		FROM api_keys AS api_key
		JOIN enterprises AS enterprise ON enterprise.id = $1
		WHERE api_key.id = $2 AND api_key.deleted_at IS NULL
		FOR UPDATE OF api_key, enterprise
		`, params.EnterpriseID, params.APIKeyID).Scan(&keyStatus, &apiKey, &keyUserID, &enterpriseUserID); err != nil {
		return err
	}
	if keyUserID != enterpriseUserID {
		return ErrEnterpriseOwnership
	}
	wasDisabled := keyStatus == "disabled"
	if !wasDisabled {
		if _, err = tx.ExecContext(ctx, `
			UPDATE api_keys SET status = 'disabled', updated_at = clock_timestamp()
			WHERE id = $1
		`, params.APIKeyID); err != nil {
			return err
		}
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE enterprise_key_assignments
		SET status = 'revoked', ended_at = NOW(), revoked_at = NOW(),
		    actor_ref = $2, updated_at = clock_timestamp()
		WHERE api_key_id = $1 AND status = 'active'
	`, params.APIKeyID, params.ActorRef)
	if err != nil {
		return err
	}
	if affected, rowsErr := result.RowsAffected(); rowsErr != nil {
		return rowsErr
	} else if affected == 0 && wasDisabled {
		if err = tx.Commit(); err != nil {
			return err
		}
		r.authCacheInvalidator.InvalidateAuthCacheByKey(ctx, apiKey)
		return nil
	} else if affected != 1 {
		return ErrKeyGenerationRevoked
	}

	if _, err = tx.ExecContext(ctx, `
		INSERT INTO enterprise_audit_events (
			enterprise_id, event_type, entity_type, entity_id, payload, actor_ref
		) VALUES ($1, 'key.generation_revoked', 'api_key', $2, '{}'::jsonb, $3)
	`, params.EnterpriseID, params.APIKeyID, params.ActorRef); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	r.authCacheInvalidator.InvalidateAuthCacheByKey(ctx, apiKey)
	return nil
}

func (r *Repository) CreateUsageAttributionSnapshot(
	ctx context.Context,
	params CreateUsageAttributionSnapshotParams,
) ([]UsageAttribution, error) {
	if params.Classification != "employee" || params.EmployeeID == nil || params.RequestAt.IsZero() {
		return nil, ErrUsageAttributionMismatch
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var generation int64
	var anchor5h, anchor7d time.Time
	err = tx.QueryRowContext(ctx, `
		SELECT assignment.generation,
		       subscription_window.window_start
		         + FLOOR(EXTRACT(EPOCH FROM ($7::timestamptz - subscription_window.window_start)) / 18000)
		           * INTERVAL '5 hours',
		       subscription_window.window_start
		FROM enterprise_subscriptions AS enterprise_subscription
		JOIN enterprises AS enterprise
		  ON enterprise.id = enterprise_subscription.enterprise_id
		 AND enterprise.dedicated_upstream_user_id = $6
		 AND enterprise.status = 'active'
		JOIN enterprise_subscription_windows AS subscription_window
		  ON subscription_window.enterprise_id = enterprise_subscription.enterprise_id
		 AND subscription_window.subscription_id = enterprise_subscription.id
		 AND subscription_window.observed_weekly_window_start = enterprise_subscription.observed_weekly_window_start
		 AND $7::timestamptz >= subscription_window.window_start
		 AND $7::timestamptz < subscription_window.window_end
		JOIN enterprise_key_assignments AS assignment
		  ON assignment.enterprise_id = enterprise_subscription.enterprise_id
		 AND assignment.employee_id = $3
		 AND assignment.api_key_id = $4
		 AND assignment.upstream_user_subscription_id = $5
		 AND assignment.assigned_at <= $7::timestamptz
		 AND (assignment.ended_at IS NULL OR $7::timestamptz < assignment.ended_at)
		WHERE enterprise_subscription.enterprise_id = $1
		  AND enterprise_subscription.id = $2
		  AND enterprise_subscription.upstream_user_subscription_id = $5
		FOR KEY SHARE OF enterprise_subscription, subscription_window, assignment
	`, params.EnterpriseID, params.SubscriptionID, *params.EmployeeID, params.APIKeyID,
		params.UpstreamSubscriptionID, params.RequesterUserID, params.RequestAt.UTC()).Scan(
		&generation, &anchor5h, &anchor7d,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrUsageAttributionMismatch
		}
		return nil, err
	}

	anchors := []struct {
		windowType string
		anchor     time.Time
	}{{WindowType5h, anchor5h.UTC()}, {WindowType7d, anchor7d.UTC()}}
	attributions := make([]UsageAttribution, 0, len(anchors))
	for _, window := range anchors {
		attribution := UsageAttribution{}
		var employeeID sql.NullInt64
		err = tx.QueryRowContext(ctx, `
				INSERT INTO enterprise_usage_attributions (
					enterprise_id, subscription_id, employee_id, api_key_id, usage_log_id,
					assignment_generation, window_type, window_anchor, request_at, classification
				) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'employee')
				ON CONFLICT (usage_log_id, window_type) DO NOTHING
				RETURNING id, enterprise_id, subscription_id, employee_id, api_key_id, usage_log_id,
			          assignment_generation, window_type, window_anchor, request_at, classification, created_at
		`, params.EnterpriseID, params.SubscriptionID, params.EmployeeID, params.APIKeyID,
			params.UsageLogID, generation, window.windowType, window.anchor, params.RequestAt.UTC()).Scan(
			&attribution.ID, &attribution.EnterpriseID, &attribution.SubscriptionID, &employeeID,
			&attribution.APIKeyID, &attribution.UsageLogID, &attribution.AssignmentGeneration,
			&attribution.WindowType, &attribution.WindowAnchor, &attribution.RequestAt,
			&attribution.Classification, &attribution.CreatedAt,
		)
		if errors.Is(err, sql.ErrNoRows) {
			err = tx.QueryRowContext(ctx, `
					SELECT id, enterprise_id, subscription_id, employee_id, api_key_id, usage_log_id,
					       assignment_generation, window_type, window_anchor, request_at, classification, created_at
					FROM enterprise_usage_attributions
					WHERE usage_log_id = $1 AND window_type = $2
				`, params.UsageLogID, window.windowType).Scan(
				&attribution.ID, &attribution.EnterpriseID, &attribution.SubscriptionID, &employeeID,
				&attribution.APIKeyID, &attribution.UsageLogID, &attribution.AssignmentGeneration,
				&attribution.WindowType, &attribution.WindowAnchor, &attribution.RequestAt,
				&attribution.Classification, &attribution.CreatedAt,
			)
			if err == nil && (attribution.EnterpriseID != params.EnterpriseID ||
				attribution.SubscriptionID != params.SubscriptionID || attribution.APIKeyID != params.APIKeyID ||
				attribution.AssignmentGeneration != generation || !attribution.WindowAnchor.Equal(window.anchor) ||
				!attribution.RequestAt.Equal(params.RequestAt.UTC()) || !sameNullableInt64(nullInt64Pointer(employeeID), params.EmployeeID)) {
				return nil, ErrUsageAttributionMismatch
			}
		}
		if err != nil {
			return nil, err
		}
		attribution.EmployeeID = nullInt64Pointer(employeeID)
		attribution.WeeklyWindowAnchor = attribution.WindowAnchor
		attributions = append(attributions, attribution)
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return attributions, nil
}

func (r *Repository) CreateUsageAttribution(
	ctx context.Context,
	params CreateUsageAttributionParams,
) (*UsageAttribution, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var apiKeyID int64
	if err = tx.QueryRowContext(ctx, `
		SELECT api_key.id
		FROM usage_logs AS usage_log
		JOIN api_keys AS api_key
		  ON api_key.id = usage_log.api_key_id
		 AND api_key.deleted_at IS NULL
		WHERE usage_log.id = $1
		FOR UPDATE OF api_key
		FOR KEY SHARE OF usage_log
	`, params.UsageLogID).Scan(&apiKeyID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrUsageAttributionMismatch
		}
		return nil, err
	}

	attribution := &UsageAttribution{}
	var employeeID sql.NullInt64
	err = tx.QueryRowContext(ctx, `
		INSERT INTO enterprise_usage_attributions (
			enterprise_id, subscription_id, employee_id, api_key_id, usage_log_id,
			assignment_generation, window_type, window_anchor, request_at, classification
		)
		SELECT $1, enterprise_subscription.id, $3::bigint, usage_log.api_key_id, usage_log.id,
		       CASE WHEN $6::text = 'employee' THEN (
				SELECT assignment.generation
				FROM enterprise_key_assignments AS assignment
				WHERE assignment.enterprise_id = $1
				  AND assignment.employee_id = $3
				  AND assignment.api_key_id = usage_log.api_key_id
				  AND assignment.upstream_user_subscription_id = usage_log.subscription_id
				  AND assignment.assigned_at <= usage_log.created_at
				  AND (assignment.ended_at IS NULL OR usage_log.created_at < assignment.ended_at)
				ORDER BY assignment.generation DESC LIMIT 1
		       ) ELSE 0 END,
		       '7d', $5::timestamptz, usage_log.created_at, $6::text
		FROM usage_logs AS usage_log
		JOIN enterprise_subscriptions AS enterprise_subscription
		  ON enterprise_subscription.id = $2
		 AND enterprise_subscription.enterprise_id = $1
		 AND enterprise_subscription.upstream_user_subscription_id = usage_log.subscription_id
		JOIN enterprises AS enterprise
		  ON enterprise.id = $1
		 AND enterprise.dedicated_upstream_user_id = usage_log.user_id
		JOIN enterprise_subscription_windows AS subscription_window
		  ON subscription_window.enterprise_id = $1
		 AND subscription_window.subscription_id = enterprise_subscription.id
		 AND subscription_window.upstream_user_subscription_id = usage_log.subscription_id
		 AND subscription_window.observed_weekly_window_start = $5::timestamptz
		 AND usage_log.created_at >= subscription_window.window_start
		 AND usage_log.created_at < subscription_window.window_end
		WHERE usage_log.id = $4
		  AND (
				(
					$6::text = 'controlled_external'
					AND $3::bigint IS NULL
					AND NOT EXISTS (
						SELECT 1
						FROM enterprise_key_assignments AS assignment
						WHERE assignment.enterprise_id = $1
						  AND assignment.api_key_id = usage_log.api_key_id
						  AND assignment.assigned_at <= usage_log.created_at
						  AND (assignment.ended_at IS NULL OR usage_log.created_at < assignment.ended_at)
					)
				)
			OR (
				$6::text = 'employee' AND $3::bigint IS NOT NULL AND EXISTS (
					SELECT 1
					FROM enterprise_key_assignments AS assignment
					WHERE assignment.enterprise_id = $1
					  AND assignment.employee_id = $3
					  AND assignment.api_key_id = usage_log.api_key_id
					  AND assignment.upstream_user_subscription_id = usage_log.subscription_id
					  AND assignment.assigned_at <= usage_log.created_at
					  AND (assignment.ended_at IS NULL OR usage_log.created_at < assignment.ended_at)
				)
			)
		  )
		ON CONFLICT (usage_log_id, window_type) DO NOTHING
		RETURNING id, enterprise_id, subscription_id, employee_id, api_key_id, usage_log_id,
		          assignment_generation, window_type, window_anchor, request_at, classification, created_at
	`, params.EnterpriseID, params.SubscriptionID, params.EmployeeID, params.UsageLogID,
		params.WeeklyWindowAnchor, params.Classification).Scan(
		&attribution.ID,
		&attribution.EnterpriseID,
		&attribution.SubscriptionID,
		&employeeID,
		&attribution.APIKeyID,
		&attribution.UsageLogID,
		&attribution.AssignmentGeneration,
		&attribution.WindowType,
		&attribution.WindowAnchor,
		&attribution.RequestAt,
		&attribution.Classification,
		&attribution.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		existing, existingErr := getUsageAttributionByUsageLogID(ctx, tx, params.UsageLogID)
		if existingErr == nil && existing.EnterpriseID == params.EnterpriseID &&
			existing.SubscriptionID == params.SubscriptionID &&
			existing.WindowAnchor.Equal(params.WeeklyWindowAnchor) &&
			existing.Classification == params.Classification &&
			sameNullableInt64(existing.EmployeeID, params.EmployeeID) {
			if err = tx.Commit(); err != nil {
				return nil, err
			}
			return existing, nil
		}
		if existingErr == nil || errors.Is(existingErr, sql.ErrNoRows) {
			return nil, ErrUsageAttributionMismatch
		}
		return nil, existingErr
	}
	if err != nil {
		return nil, err
	}
	attribution.EmployeeID = nullInt64Pointer(employeeID)
	attribution.WeeklyWindowAnchor = attribution.WindowAnchor
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return attribution, nil
}

func (r *Repository) GetUsageAttributionByUsageLogID(
	ctx context.Context,
	usageLogID int64,
) (*UsageAttribution, error) {
	return getUsageAttributionByUsageLogID(ctx, r.db, usageLogID)
}

type queryRower interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func getUsageAttributionByUsageLogID(
	ctx context.Context,
	queryer queryRower,
	usageLogID int64,
) (*UsageAttribution, error) {
	attribution := &UsageAttribution{}
	var employeeID sql.NullInt64
	err := queryer.QueryRowContext(ctx, `
		SELECT id, enterprise_id, subscription_id, employee_id, api_key_id, usage_log_id,
		       assignment_generation, window_type, window_anchor, request_at, classification, created_at
		FROM enterprise_usage_attributions
		WHERE usage_log_id = $1 AND window_type = '7d'
	`, usageLogID).Scan(
		&attribution.ID,
		&attribution.EnterpriseID,
		&attribution.SubscriptionID,
		&employeeID,
		&attribution.APIKeyID,
		&attribution.UsageLogID,
		&attribution.AssignmentGeneration,
		&attribution.WindowType,
		&attribution.WindowAnchor,
		&attribution.RequestAt,
		&attribution.Classification,
		&attribution.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	attribution.EmployeeID = nullInt64Pointer(employeeID)
	attribution.WeeklyWindowAnchor = attribution.WindowAnchor
	return attribution, nil
}

func (r *Repository) ListAllocationRevisions(
	ctx context.Context,
	allocationID int64,
) ([]AllocationRevision, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, allocation_id, version, previous_amount::text, new_amount::text,
		       reason, actor_ref, created_at
		FROM enterprise_allocation_revisions
		WHERE allocation_id = $1
		ORDER BY version
	`, allocationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var revisions []AllocationRevision
	for rows.Next() {
		var revision AllocationRevision
		var previousAmount sql.NullString
		if err = rows.Scan(
			&revision.ID,
			&revision.AllocationID,
			&revision.Version,
			&previousAmount,
			&revision.NewAmount,
			&revision.Reason,
			&revision.ActorRef,
			&revision.CreatedAt,
		); err != nil {
			return nil, err
		}
		if previousAmount.Valid {
			revision.PreviousAmount = &previousAmount.String
		}
		revisions = append(revisions, revision)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return revisions, nil
}

func (r *Repository) ListAuditEvents(
	ctx context.Context,
	enterpriseID int64,
) ([]AuditEvent, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, event_type, entity_type, entity_id, payload, actor_ref, created_at
		FROM enterprise_audit_events
		WHERE enterprise_id = $1
		ORDER BY created_at, id
	`, enterpriseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []AuditEvent
	for rows.Next() {
		var event AuditEvent
		var entityID sql.NullInt64
		if err = rows.Scan(
			&event.ID,
			&event.EventType,
			&event.EntityType,
			&entityID,
			&event.Payload,
			&event.ActorRef,
			&event.CreatedAt,
		); err != nil {
			return nil, err
		}
		event.EntityID = nullInt64Pointer(entityID)
		events = append(events, event)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return events, nil
}

func sameNullableTime(expected *time.Time, actual sql.NullTime) bool {
	if expected == nil {
		return !actual.Valid
	}
	return actual.Valid && expected.Equal(actual.Time)
}

func nullTimePointer(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	result := value.Time
	return &result
}

func nullInt64Pointer(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	result := value.Int64
	return &result
}

func sameNullableInt64(left, right *int64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
