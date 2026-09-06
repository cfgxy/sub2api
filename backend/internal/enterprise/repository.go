package enterprise

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

var (
	ErrAllocationVersionConflict = errors.New("enterprise allocation version conflict")
	ErrActorRefRequired          = errors.New("enterprise actor ref is required")
	ErrReasonRequired            = errors.New("enterprise allocation reason is required")
)

type Repository struct {
	db *sql.DB
}

type Allocation struct {
	ID                 int64
	EnterpriseID       int64
	SubscriptionID     int64
	WeeklyWindowAnchor time.Time
	EmployeeID         int64
	Amount             string
	Version            int64
	CreatedAt          time.Time
	UpdatedAt          time.Time
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
	EnterpriseID       int64
	SubscriptionID     int64
	WeeklyWindowAnchor time.Time
	EmployeeID         int64
}

type AllocationUsageSummary struct {
	Allocation string
	ActualCost string
	Remaining  string
	Overage    string
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
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
			enterprise_id, subscription_id, weekly_window_anchor, employee_id, amount, version
		) VALUES ($1, $2, $3, $4, $5::numeric, 1)
		RETURNING id, enterprise_id, subscription_id, weekly_window_anchor, employee_id,
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
		SELECT id, enterprise_id, subscription_id, weekly_window_anchor, employee_id,
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
		RETURNING id, enterprise_id, subscription_id, weekly_window_anchor, employee_id,
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
	summary := &AllocationUsageSummary{}
	err := r.db.QueryRowContext(ctx, `
		WITH usage_total AS (
			SELECT COALESCE(SUM(usage_logs.actual_cost), 0)::NUMERIC(20,10) AS actual_cost
			FROM enterprise_usage_attributions
			JOIN usage_logs ON usage_logs.id = enterprise_usage_attributions.usage_log_id
			WHERE enterprise_usage_attributions.enterprise_id = $1
			  AND enterprise_usage_attributions.subscription_id = $2
			  AND enterprise_usage_attributions.weekly_window_anchor = $3
			  AND enterprise_usage_attributions.employee_id = $4
			  AND enterprise_usage_attributions.classification = 'employee'
		)
		SELECT allocation.amount::text,
		       usage_total.actual_cost::text,
		       GREATEST(allocation.amount - usage_total.actual_cost, 0)::NUMERIC(20,10)::text,
		       GREATEST(usage_total.actual_cost - allocation.amount, 0)::NUMERIC(20,10)::text
		FROM enterprise_weekly_allocations AS allocation
		CROSS JOIN usage_total
		WHERE allocation.enterprise_id = $1
		  AND allocation.subscription_id = $2
		  AND allocation.weekly_window_anchor = $3
		  AND allocation.employee_id = $4
	`, query.EnterpriseID, query.SubscriptionID, query.WeeklyWindowAnchor, query.EmployeeID).Scan(
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
