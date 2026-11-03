package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

const (
	enterpriseEmployeeReservationStatusReserved  = "reserved"
	enterpriseEmployeeReservationStatusCommitted = "committed"
	enterpriseEmployeeReservationStatusSettled   = "settled"
	enterpriseEmployeeReservationStatusReleased  = "released"
)

type usageBillingRepository struct {
	db *sql.DB
}

func NewUsageBillingRepository(_ *dbent.Client, sqlDB *sql.DB) service.UsageBillingRepository {
	return &usageBillingRepository{db: sqlDB}
}

func (r *usageBillingRepository) Apply(ctx context.Context, cmd *service.UsageBillingCommand) (_ *service.UsageBillingApplyResult, err error) {
	if cmd == nil {
		return &service.UsageBillingApplyResult{}, nil
	}
	if r == nil || r.db == nil {
		return nil, errors.New("usage billing repository db is nil")
	}

	cmd.Normalize()
	if cmd.RequestID == "" {
		return nil, service.ErrUsageBillingRequestIDRequired
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	applied, err := r.claimUsageBillingKey(ctx, tx, cmd)
	if err != nil {
		return nil, err
	}
	if !applied {
		return &service.UsageBillingApplyResult{Applied: false}, nil
	}

	result := &service.UsageBillingApplyResult{Applied: true}
	if err := r.applyUsageBillingEffects(ctx, tx, cmd, result); err != nil {
		return nil, err
	}
	if cmd.EnterpriseEmployeeReservation {
		if err := persistEnterpriseEmployeeCommittedSettlement(ctx, tx, cmd); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil
	return result, nil
}

// persistEnterpriseEmployeeCommittedSettlement writes the exact charge in the
// same transaction as the core billing effects. The later employee-side
// settlement may retry safely after a process crash without replaying billing.
func persistEnterpriseEmployeeCommittedSettlement(ctx context.Context, tx *sql.Tx, cmd *service.UsageBillingCommand) error {
	if cmd == nil || cmd.EnterpriseEmployeeActualUSD < 0 {
		return service.ErrEnterpriseEmployeeReservationStateConflict
	}

	var reservationID int64
	err := tx.QueryRowContext(ctx, `
		UPDATE enterprise_employee_usage_reservations
		SET actual_usd = $1, status = 'committed', committed_at = NOW(), updated_at = NOW()
		WHERE request_id = $2 AND source_api_key_id = $3 AND status = 'reserved'
		  AND reserved_usd + 0.000000001 >= $1
		RETURNING id`, cmd.EnterpriseEmployeeActualUSD, cmd.RequestID, cmd.APIKeyID).Scan(&reservationID)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	var reservedUSD float64
	var status string
	err = tx.QueryRowContext(ctx, `
		SELECT reserved_usd, status
		FROM enterprise_employee_usage_reservations
		WHERE request_id = $1 AND source_api_key_id = $2`, cmd.RequestID, cmd.APIKeyID).Scan(&reservedUSD, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return service.ErrEnterpriseEmployeeReservationNotFound
	}
	if err != nil {
		return err
	}
	if status == enterpriseEmployeeReservationStatusReserved && cmd.EnterpriseEmployeeActualUSD > reservedUSD+0.000000001 {
		return service.ErrEnterpriseEmployeeSettlementExceedsReservation
	}
	return service.ErrEnterpriseEmployeeReservationStateConflict
}

func (r *usageBillingRepository) ReserveEnterpriseEmployeeUsage(ctx context.Context, cmd *service.EnterpriseEmployeeUsageReservationCommand) (_ *service.EnterpriseEmployeeUsageReservationResult, err error) {
	if cmd == nil {
		return nil, service.ErrEnterpriseEmployeeReservationNotFound
	}
	if r == nil || r.db == nil {
		return nil, errors.New("usage billing repository db is nil")
	}
	cmd.Normalize()
	if cmd.RequestID == "" {
		return nil, service.ErrUsageBillingRequestIDRequired
	}
	if len(cmd.RequestFingerprint) != 64 || cmd.SourceAPIKeyID <= 0 || cmd.ReservedUSD <= 0 {
		return nil, service.ErrEnterpriseEmployeeReservationStateConflict
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	var enterpriseID, employeeID int64
	err = tx.QueryRowContext(ctx, `
		SELECT enterprise_id, employee_id
		FROM enterprise_key_assignments
		WHERE api_key_id = $1 AND status = 'active'
		ORDER BY generation DESC, id DESC
		LIMIT 1`, cmd.SourceAPIKeyID).Scan(&enterpriseID, &employeeID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrEnterpriseAttributionUnavailable
	}
	if err != nil {
		return nil, err
	}

	var ignored int64
	if err = tx.QueryRowContext(ctx, `
		SELECT id FROM enterprise_employees
		WHERE enterprise_id = $1 AND id = $2 AND status = 'active'
		FOR UPDATE`, enterpriseID, employeeID).Scan(&ignored); err != nil {
		return nil, service.ErrEnterpriseAttributionUnavailable
	}
	if err = tx.QueryRowContext(ctx, `
		SELECT id FROM enterprises WHERE id = $1 AND status = 'active' FOR UPDATE`, enterpriseID).Scan(&ignored); err != nil {
		return nil, service.ErrEnterpriseAttributionUnavailable
	}

	var generation, upstreamSubscriptionID, upstreamGroupID int64
	var usage7d, rateLimit7d float64
	var keyWindow time.Time
	err = tx.QueryRowContext(ctx, `
		SELECT assignment.generation, assignment.upstream_user_subscription_id,
		       assignment.upstream_group_id, api_key.usage_7d, api_key.rate_limit_7d,
		       api_key.window_7d_start
		FROM enterprise_key_assignments AS assignment
		JOIN api_keys AS api_key ON api_key.id = assignment.api_key_id AND api_key.deleted_at IS NULL
		WHERE assignment.enterprise_id = $1 AND assignment.employee_id = $2
		  AND assignment.api_key_id = $3 AND assignment.status = 'active'
		FOR UPDATE OF assignment, api_key`, enterpriseID, employeeID, cmd.SourceAPIKeyID).
		Scan(&generation, &upstreamSubscriptionID, &upstreamGroupID, &usage7d, &rateLimit7d, &keyWindow)
	if err != nil {
		return nil, service.ErrEnterpriseAttributionUnavailable
	}

	var enterpriseSubscriptionID int64
	var anchor time.Time
	err = tx.QueryRowContext(ctx, `
		SELECT enterprise_subscription.id, upstream_subscription.weekly_window_start
		FROM enterprise_subscriptions AS enterprise_subscription
		JOIN user_subscriptions AS upstream_subscription
		  ON upstream_subscription.id = enterprise_subscription.upstream_user_subscription_id
		 AND upstream_subscription.deleted_at IS NULL
		WHERE enterprise_subscription.enterprise_id = $1
		  AND enterprise_subscription.upstream_user_subscription_id = $2
		  AND enterprise_subscription.status = 'active'
		  AND upstream_subscription.status = 'active'
		  AND upstream_subscription.group_id = $3
		  AND upstream_subscription.starts_at <= CURRENT_TIMESTAMP
		  AND upstream_subscription.expires_at > CURRENT_TIMESTAMP
		FOR UPDATE OF enterprise_subscription, upstream_subscription`, enterpriseID, upstreamSubscriptionID, upstreamGroupID).
		Scan(&enterpriseSubscriptionID, &anchor)
	if err != nil {
		return nil, service.ErrEnterpriseAttributionUnavailable
	}
	anchor = anchor.UTC()
	if !keyWindow.UTC().Equal(anchor) {
		return nil, service.ErrEnterpriseAttributionUnavailable
	}

	var allocation float64
	err = tx.QueryRowContext(ctx, `
		SELECT amount
		FROM enterprise_weekly_allocations
		WHERE enterprise_id = $1 AND subscription_id = $2 AND employee_id = $3
		  AND window_type = 'week' AND window_anchor = $4
		FOR UPDATE`, enterpriseID, enterpriseSubscriptionID, employeeID, anchor).Scan(&allocation)
	if errors.Is(err, sql.ErrNoRows) || allocation <= 0 || rateLimit7d <= 0 {
		return nil, service.ErrAPIKeyRateLimit7dExceeded
	}
	if err != nil {
		return nil, err
	}

	if existing, existingErr := loadEnterpriseEmployeeReservationForUpdate(ctx, tx, cmd.RequestID, cmd.SourceAPIKeyID); existingErr == nil {
		if strings.TrimSpace(existing.RequestID) == "" {
			return nil, service.ErrEnterpriseEmployeeReservationStateConflict
		}
		var existingFingerprint string
		if scanErr := tx.QueryRowContext(ctx, `
			SELECT request_fingerprint FROM enterprise_employee_usage_reservations WHERE id = $1`, existing.ReservationID).Scan(&existingFingerprint); scanErr != nil {
			return nil, scanErr
		}
		if strings.TrimSpace(existingFingerprint) != cmd.RequestFingerprint {
			return nil, service.ErrUsageBillingRequestConflict
		}
		existing.AlreadyExists = true
		return existing, nil
	} else if !errors.Is(existingErr, sql.ErrNoRows) {
		return nil, existingErr
	}

	var reserved float64
	if err = tx.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(
			CASE WHEN status = 'committed' THEN actual_usd ELSE reserved_usd END
		), 0)
		FROM enterprise_employee_usage_reservations
		WHERE employee_id = $1 AND enterprise_subscription_id = $2
		  AND weekly_window_anchor = $3 AND status IN ('reserved', 'committed')`, employeeID, enterpriseSubscriptionID, anchor).Scan(&reserved); err != nil {
		return nil, err
	}
	if usage7d+reserved+cmd.ReservedUSD > allocation+0.000000001 {
		return nil, service.ErrAPIKeyRateLimit7dExceeded
	}

	result := &service.EnterpriseEmployeeUsageReservationResult{}
	err = tx.QueryRowContext(ctx, `
		INSERT INTO enterprise_employee_usage_reservations (
			request_id, request_fingerprint, source_api_key_id, enterprise_id,
			enterprise_subscription_id, upstream_user_subscription_id, employee_id,
			assignment_generation, weekly_window_anchor, reserved_usd
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id, request_id, source_api_key_id, enterprise_id,
		          enterprise_subscription_id, upstream_user_subscription_id,
		          employee_id, assignment_generation, weekly_window_anchor,
		          reserved_usd, actual_usd, status`, cmd.RequestID, cmd.RequestFingerprint,
		cmd.SourceAPIKeyID, enterpriseID, enterpriseSubscriptionID, upstreamSubscriptionID,
		employeeID, generation, anchor, cmd.ReservedUSD).
		Scan(&result.ReservationID, &result.RequestID, &result.SourceAPIKeyID, &result.EnterpriseID,
			&result.EnterpriseSubscriptionID, &result.UpstreamUserSubscriptionID, &result.EmployeeID,
			&result.AssignmentGeneration, &result.WeeklyWindowAnchor, &result.ReservedUSD,
			&result.ActualUSD, &result.Status)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil
	return result, nil
}

func (r *usageBillingRepository) SettleEnterpriseEmployeeUsage(ctx context.Context, cmd *service.EnterpriseEmployeeUsageSettlementCommand) (_ *service.EnterpriseEmployeeUsageReservationResult, err error) {
	if cmd == nil || r == nil || r.db == nil {
		return nil, service.ErrEnterpriseEmployeeReservationNotFound
	}
	cmd.Normalize()
	if cmd.RequestID == "" {
		return nil, service.ErrUsageBillingRequestIDRequired
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	reservation, err := loadEnterpriseEmployeeReservation(ctx, tx, cmd.RequestID, cmd.SourceAPIKeyID, false)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrEnterpriseEmployeeReservationNotFound
	}
	if err != nil {
		return nil, err
	}

	var ignored int64
	if err = tx.QueryRowContext(ctx, `SELECT id FROM enterprise_employees WHERE enterprise_id = $1 AND id = $2 FOR UPDATE`, reservation.EnterpriseID, reservation.EmployeeID).Scan(&ignored); err != nil {
		return nil, err
	}
	if err = tx.QueryRowContext(ctx, `SELECT id FROM enterprises WHERE id = $1 FOR UPDATE`, reservation.EnterpriseID).Scan(&ignored); err != nil {
		return nil, err
	}

	var targetAPIKeyID, targetUpstreamSubscriptionID int64
	err = tx.QueryRowContext(ctx, `
		SELECT assignment.api_key_id, assignment.upstream_user_subscription_id
		FROM enterprise_key_assignments AS assignment
		JOIN api_keys AS api_key ON api_key.id = assignment.api_key_id AND api_key.deleted_at IS NULL
		WHERE assignment.enterprise_id = $1 AND assignment.employee_id = $2
		ORDER BY CASE WHEN assignment.status = 'active' THEN 0 ELSE 1 END,
		         assignment.generation DESC, assignment.id DESC
		LIMIT 1 FOR UPDATE OF assignment, api_key`, reservation.EnterpriseID, reservation.EmployeeID).
		Scan(&targetAPIKeyID, &targetUpstreamSubscriptionID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	var currentAnchor time.Time
	err = tx.QueryRowContext(ctx, `
		SELECT upstream_subscription.weekly_window_start
		FROM enterprise_subscriptions AS enterprise_subscription
		JOIN user_subscriptions AS upstream_subscription
		  ON upstream_subscription.id = enterprise_subscription.upstream_user_subscription_id
		 AND upstream_subscription.deleted_at IS NULL
		WHERE enterprise_subscription.id = $1 AND enterprise_subscription.enterprise_id = $2
		  AND enterprise_subscription.upstream_user_subscription_id = $3
		FOR UPDATE OF enterprise_subscription, upstream_subscription`, reservation.EnterpriseSubscriptionID,
		reservation.EnterpriseID, reservation.UpstreamUserSubscriptionID).Scan(&currentAnchor)
	if err != nil {
		return nil, err
	}
	reservation, err = loadEnterpriseEmployeeReservationForUpdate(ctx, tx, cmd.RequestID, cmd.SourceAPIKeyID)
	if err != nil {
		return nil, err
	}
	if reservation.Status == enterpriseEmployeeReservationStatusSettled {
		if reservation.ActualUSD != nil && absFloat(*reservation.ActualUSD-cmd.ActualUSD) <= 0.000000001 {
			return reservation, nil
		}
		return nil, service.ErrEnterpriseEmployeeReservationStateConflict
	}
	actualUSD := cmd.ActualUSD
	if reservation.Status == enterpriseEmployeeReservationStatusCommitted {
		if reservation.ActualUSD == nil {
			return nil, service.ErrEnterpriseEmployeeReservationStateConflict
		}
		actualUSD = *reservation.ActualUSD
	} else if reservation.Status != enterpriseEmployeeReservationStatusReserved {
		return nil, service.ErrEnterpriseEmployeeReservationStateConflict
	}
	if actualUSD > reservation.ReservedUSD+0.000000001 {
		return nil, service.ErrEnterpriseEmployeeSettlementExceedsReservation
	}

	if actualUSD > 0 && targetAPIKeyID > 0 && targetUpstreamSubscriptionID == reservation.UpstreamUserSubscriptionID && currentAnchor.UTC().Equal(reservation.WeeklyWindowAnchor.UTC()) {
		if _, err = tx.ExecContext(ctx, `
			UPDATE api_keys
			SET usage_7d = usage_7d + $1, window_7d_start = $2, updated_at = NOW()
			WHERE id = $3 AND deleted_at IS NULL`, actualUSD, reservation.WeeklyWindowAnchor, targetAPIKeyID); err != nil {
			return nil, err
		}
		reservation.BilledAPIKeyID = targetAPIKeyID
	}
	if _, err = tx.ExecContext(ctx, `
		UPDATE enterprise_employee_usage_reservations
		SET actual_usd = $1, status = 'settled',
		    committed_at = COALESCE(committed_at, NOW()), settled_at = NOW(), updated_at = NOW()
		WHERE id = $2`, actualUSD, reservation.ReservationID); err != nil {
		return nil, err
	}
	actual := actualUSD
	reservation.ActualUSD = &actual
	reservation.Status = enterpriseEmployeeReservationStatusSettled
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil
	return reservation, nil
}

func (r *usageBillingRepository) ReleaseEnterpriseEmployeeUsage(ctx context.Context, cmd *service.EnterpriseEmployeeUsageReleaseCommand) (_ *service.EnterpriseEmployeeUsageReservationResult, err error) {
	if cmd == nil || r == nil || r.db == nil {
		return nil, service.ErrEnterpriseEmployeeReservationNotFound
	}
	cmd.RequestID = strings.TrimSpace(cmd.RequestID)
	if cmd.RequestID == "" {
		return nil, service.ErrUsageBillingRequestIDRequired
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()
	reservation, err := loadEnterpriseEmployeeReservationForUpdate(ctx, tx, cmd.RequestID, cmd.SourceAPIKeyID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrEnterpriseEmployeeReservationNotFound
	}
	if err != nil {
		return nil, err
	}
	if reservation.Status != enterpriseEmployeeReservationStatusReserved {
		reservation.BillingCommitted = reservation.Status == enterpriseEmployeeReservationStatusCommitted
		return reservation, nil
	}
	if _, err = tx.ExecContext(ctx, `
		UPDATE enterprise_employee_usage_reservations
		SET status = 'released', released_at = NOW(), updated_at = NOW()
		WHERE id = $1`, reservation.ReservationID); err != nil {
		return nil, err
	}
	reservation.Status = enterpriseEmployeeReservationStatusReleased
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil
	return reservation, nil
}

// RecoverEnterpriseEmployeeUsage settles committed reservations for the same
// employee as sourceAPIKeyID. It deliberately invokes the normal settlement
// transaction for each row, preserving the employee -> assignment ->
// subscription locking order and settlement idempotency.
func (r *usageBillingRepository) RecoverEnterpriseEmployeeUsage(ctx context.Context, sourceAPIKeyID int64) error {
	if r == nil || r.db == nil || sourceAPIKeyID <= 0 {
		return service.ErrEnterpriseEmployeeReservationNotFound
	}

	var enterpriseID, employeeID int64
	err := r.db.QueryRowContext(ctx, `
		SELECT enterprise_id, employee_id
		FROM enterprise_key_assignments
		WHERE api_key_id = $1
		ORDER BY generation DESC, id DESC
		LIMIT 1`, sourceAPIKeyID).Scan(&enterpriseID, &employeeID)
	if errors.Is(err, sql.ErrNoRows) {
		return service.ErrEnterpriseAttributionUnavailable
	}
	if err != nil {
		return err
	}

	rows, err := r.db.QueryContext(ctx, `
		SELECT request_id, source_api_key_id, actual_usd
		FROM enterprise_employee_usage_reservations
		WHERE enterprise_id = $1 AND employee_id = $2 AND status = 'committed'
		ORDER BY committed_at, id`, enterpriseID, employeeID)
	if err != nil {
		return err
	}
	defer rows.Close()

	type pendingSettlement struct {
		requestID      string
		sourceAPIKeyID int64
		actualUSD      float64
	}
	var pending []pendingSettlement
	for rows.Next() {
		var item pendingSettlement
		if err := rows.Scan(&item.requestID, &item.sourceAPIKeyID, &item.actualUSD); err != nil {
			return err
		}
		pending = append(pending, item)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, item := range pending {
		if _, err := r.SettleEnterpriseEmployeeUsage(ctx, &service.EnterpriseEmployeeUsageSettlementCommand{
			RequestID: item.requestID, SourceAPIKeyID: item.sourceAPIKeyID, ActualUSD: item.actualUSD,
		}); err != nil {
			return err
		}
	}
	return nil
}

func loadEnterpriseEmployeeReservationForUpdate(ctx context.Context, tx *sql.Tx, requestID string, sourceAPIKeyID int64) (*service.EnterpriseEmployeeUsageReservationResult, error) {
	return loadEnterpriseEmployeeReservation(ctx, tx, requestID, sourceAPIKeyID, true)
}

func loadEnterpriseEmployeeReservation(ctx context.Context, tx *sql.Tx, requestID string, sourceAPIKeyID int64, forUpdate bool) (*service.EnterpriseEmployeeUsageReservationResult, error) {
	query := `
		SELECT id, request_id, source_api_key_id, enterprise_id,
		       enterprise_subscription_id, upstream_user_subscription_id,
		       employee_id, assignment_generation, weekly_window_anchor,
		       reserved_usd, actual_usd, status
		FROM enterprise_employee_usage_reservations
		WHERE request_id = $1 AND source_api_key_id = $2`
	if forUpdate {
		query += " FOR UPDATE"
	}
	result := &service.EnterpriseEmployeeUsageReservationResult{}
	err := tx.QueryRowContext(ctx, query, requestID, sourceAPIKeyID).Scan(
		&result.ReservationID, &result.RequestID, &result.SourceAPIKeyID, &result.EnterpriseID,
		&result.EnterpriseSubscriptionID, &result.UpstreamUserSubscriptionID, &result.EmployeeID,
		&result.AssignmentGeneration, &result.WeeklyWindowAnchor, &result.ReservedUSD,
		&result.ActualUSD, &result.Status,
	)
	return result, err
}

func usageBillingClaimExists(ctx context.Context, tx *sql.Tx, requestID string, apiKeyID int64) (bool, error) {
	var exists int
	err := tx.QueryRowContext(ctx, `
		SELECT 1 FROM usage_billing_dedup WHERE request_id = $1 AND api_key_id = $2
		UNION ALL
		SELECT 1 FROM usage_billing_dedup_archive WHERE request_id = $1 AND api_key_id = $2
		LIMIT 1`, requestID, apiKeyID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func absFloat(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func (r *usageBillingRepository) claimUsageBillingKey(ctx context.Context, tx *sql.Tx, cmd *service.UsageBillingCommand) (bool, error) {
	return r.claimUsageBillingRequest(ctx, tx, cmd.RequestID, cmd.APIKeyID, cmd.RequestFingerprint)
}

func (r *usageBillingRepository) claimUsageBillingRequest(ctx context.Context, tx *sql.Tx, requestID string, apiKeyID int64, requestFingerprint string) (bool, error) {
	var id int64
	err := tx.QueryRowContext(ctx, `
		INSERT INTO usage_billing_dedup (request_id, api_key_id, request_fingerprint)
		VALUES ($1, $2, $3)
		ON CONFLICT (request_id, api_key_id) DO NOTHING
		RETURNING id
	`, requestID, apiKeyID, requestFingerprint).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		var existingFingerprint string
		if err := tx.QueryRowContext(ctx, `
			SELECT request_fingerprint
			FROM usage_billing_dedup
			WHERE request_id = $1 AND api_key_id = $2
		`, requestID, apiKeyID).Scan(&existingFingerprint); err != nil {
			return false, err
		}
		if strings.TrimSpace(existingFingerprint) != strings.TrimSpace(requestFingerprint) {
			return false, service.ErrUsageBillingRequestConflict
		}
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var archivedFingerprint string
	err = tx.QueryRowContext(ctx, `
		SELECT request_fingerprint
		FROM usage_billing_dedup_archive
		WHERE request_id = $1 AND api_key_id = $2
	`, requestID, apiKeyID).Scan(&archivedFingerprint)
	if err == nil {
		if strings.TrimSpace(archivedFingerprint) != strings.TrimSpace(requestFingerprint) {
			return false, service.ErrUsageBillingRequestConflict
		}
		return false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	return true, nil
}

func (r *usageBillingRepository) ReserveBatchImageBalance(ctx context.Context, cmd *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error) {
	return r.applyBatchImageBalanceHold(ctx, cmd, reserveUsageBillingBatchImageBalance)
}

func (r *usageBillingRepository) CaptureBatchImageBalance(ctx context.Context, cmd *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error) {
	return r.applyBatchImageBalanceHold(ctx, cmd, captureUsageBillingBatchImageBalance)
}

func (r *usageBillingRepository) ReleaseBatchImageBalance(ctx context.Context, cmd *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error) {
	return r.applyBatchImageBalanceHold(ctx, cmd, releaseUsageBillingBatchImageBalance)
}

func (r *usageBillingRepository) applyBatchImageBalanceHold(
	ctx context.Context,
	cmd *service.BatchImageBalanceHoldCommand,
	apply func(context.Context, *sql.Tx, *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error),
) (_ *service.BatchImageBalanceHoldResult, err error) {
	if cmd == nil {
		return &service.BatchImageBalanceHoldResult{}, nil
	}
	if r == nil || r.db == nil {
		return nil, errors.New("usage billing repository db is nil")
	}
	cmd.Normalize()
	if cmd.RequestID == "" {
		return nil, service.ErrUsageBillingRequestIDRequired
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	applied, err := r.claimUsageBillingRequest(ctx, tx, cmd.RequestID, cmd.APIKeyID, cmd.RequestFingerprint)
	if err != nil {
		return nil, err
	}
	if !applied {
		return &service.BatchImageBalanceHoldResult{Applied: false}, nil
	}

	result, err := apply(ctx, tx, cmd)
	if err != nil {
		return nil, err
	}
	if result == nil {
		result = &service.BatchImageBalanceHoldResult{}
	}
	result.Applied = true

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil
	return result, nil
}

func (r *usageBillingRepository) applyUsageBillingEffects(ctx context.Context, tx *sql.Tx, cmd *service.UsageBillingCommand, result *service.UsageBillingApplyResult) error {
	apiKeyID := cmd.APIKeyID
	if cmd.APIKeyQuotaCost > 0 || cmd.APIKeyRateLimitCost > 0 {
		var err error
		// 与员工 Key 生命周期统一采用员工→Key→订阅的锁顺序，避免订阅计费和并发轮换死锁。
		apiKeyID, err = resolveUsageBillingAPIKeyID(ctx, tx, cmd.APIKeyID)
		if err != nil {
			return err
		}
		result.BilledAPIKeyID = apiKeyID
	}

	if cmd.SubscriptionCost > 0 && cmd.SubscriptionID != nil {
		if err := incrementUsageBillingSubscription(ctx, tx, *cmd.SubscriptionID, cmd.SubscriptionCost); err != nil {
			return err
		}
	}

	if cmd.BalanceCost > 0 {
		newBalance, sufficient, err := deductUsageBillingBalance(ctx, tx, cmd.UserID, cmd.BalanceCost)
		if err != nil {
			return err
		}
		result.NewBalance = &newBalance
		result.BalanceOverdrafted = !sufficient
	}

	// Key 已不存在时跳过其自身的额度/限速计数，其余结算项不受影响。
	if cmd.APIKeyQuotaCost > 0 {
		exhausted, err := incrementUsageBillingAPIKeyQuota(ctx, tx, apiKeyID, cmd.APIKeyQuotaCost)
		if err != nil && !errors.Is(err, service.ErrAPIKeyNotFound) {
			return err
		}
		result.APIKeyQuotaExhausted = exhausted
	}

	if cmd.APIKeyRateLimitCost > 0 && !cmd.EnterpriseEmployeeReservation {
		if err := incrementUsageBillingAPIKeyRateLimit(ctx, tx, apiKeyID, cmd.APIKeyRateLimitCost); err != nil && !errors.Is(err, service.ErrAPIKeyNotFound) {
			return err
		}
	}

	if cmd.AccountQuotaCost > 0 && (strings.EqualFold(cmd.AccountType, service.AccountTypeAPIKey) || strings.EqualFold(cmd.AccountType, service.AccountTypeBedrock)) {
		quotaState, err := incrementUsageBillingAccountQuota(ctx, tx, cmd.AccountID, cmd.AccountQuotaCost)
		if err != nil {
			return err
		}
		result.QuotaState = quotaState
	}

	return nil
}

// resolveUsageBillingAPIKeyID serializes billing with employee Key lifecycle
// changes. A billing request that started before rotation keeps its original
// target until the lifecycle transaction copies the final counters; a request
// that arrives after rotation is charged to the current active successor.
func resolveUsageBillingAPIKeyID(ctx context.Context, tx *sql.Tx, apiKeyID int64) (int64, error) {
	var enterpriseID, employeeID int64
	err := tx.QueryRowContext(ctx, `
		SELECT enterprise_id, employee_id
		FROM enterprise_key_assignments
		WHERE api_key_id = $1
		ORDER BY generation DESC
		LIMIT 1`, apiKeyID).Scan(&enterpriseID, &employeeID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}

	targetAPIKeyID := apiKeyID
	enterpriseKey := err == nil
	if enterpriseKey {
		var lockedEmployeeID int64
		if err := tx.QueryRowContext(ctx, `
			SELECT id
			FROM enterprise_employees
			WHERE enterprise_id = $1 AND id = $2
			FOR UPDATE`, enterpriseID, employeeID).Scan(&lockedEmployeeID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return 0, service.ErrAPIKeyNotFound
			}
			return 0, err
		}

		err = tx.QueryRowContext(ctx, `
			SELECT api_key_id
			FROM enterprise_key_assignments
			WHERE enterprise_id = $1 AND employee_id = $2
			ORDER BY CASE WHEN status = 'active' THEN 0 ELSE 1 END, generation DESC
			LIMIT 1
			FOR UPDATE`, enterpriseID, employeeID).Scan(&targetAPIKeyID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return 0, err
		}
		if errors.Is(err, sql.ErrNoRows) {
			targetAPIKeyID = apiKeyID
		}
	}

	var lockedAPIKeyID int64
	if err := tx.QueryRowContext(ctx, `
		SELECT id
		FROM api_keys
		WHERE id = $1 AND deleted_at IS NULL
		FOR UPDATE`, targetAPIKeyID).Scan(&lockedAPIKeyID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// 普通 Key 删除后仍结算已完成请求；企业 Key 必须保留后继目标及员工锁校验。
			if !enterpriseKey {
				return apiKeyID, nil
			}
			return 0, service.ErrAPIKeyNotFound
		}
		return 0, err
	}
	return lockedAPIKeyID, nil
}

func incrementUsageBillingSubscription(ctx context.Context, tx *sql.Tx, subscriptionID int64, costUSD float64) error {
	const updateSQL = `
		UPDATE user_subscriptions us
		SET
			daily_usage_usd = us.daily_usage_usd + $1,
			weekly_usage_usd = us.weekly_usage_usd + $1,
			monthly_usage_usd = us.monthly_usage_usd + $1,
			updated_at = NOW()
		FROM groups g
		WHERE us.id = $2
			AND us.deleted_at IS NULL
			AND us.group_id = g.id
			AND g.deleted_at IS NULL
	`
	res, err := tx.ExecContext(ctx, updateSQL, costUSD, subscriptionID)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected > 0 {
		return nil
	}
	return service.ErrSubscriptionNotFound
}

func deductUsageBillingBalance(ctx context.Context, tx *sql.Tx, userID int64, amount float64) (float64, bool, error) {
	var newBalance float64
	err := tx.QueryRowContext(ctx, `
		UPDATE users
		SET balance = balance - $1,
			updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL AND balance >= $1
		RETURNING balance
	`, amount, userID).Scan(&newBalance)
	if err == nil {
		return newBalance, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, false, err
	}

	err = tx.QueryRowContext(ctx, `
		UPDATE users
		SET balance = balance - $1,
			updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL
		RETURNING balance
	`, amount, userID).Scan(&newBalance)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, service.ErrUserNotFound
	}
	if err != nil {
		return 0, false, err
	}
	return newBalance, false, nil
}

func reserveUsageBillingBatchImageBalance(ctx context.Context, tx *sql.Tx, cmd *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error) {
	if cmd.HoldAmount <= 0 {
		return &service.BatchImageBalanceHoldResult{}, nil
	}
	var balance, frozen float64
	err := tx.QueryRowContext(ctx, `
		UPDATE users
		SET balance = balance - $1,
			frozen_balance = COALESCE(frozen_balance, 0) + $1,
			updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL AND balance >= $1
		RETURNING balance, frozen_balance
	`, cmd.HoldAmount, cmd.UserID).Scan(&balance, &frozen)
	if err == nil {
		return &service.BatchImageBalanceHoldResult{NewBalance: &balance, FrozenBalance: &frozen}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if exists, existsErr := userExistsForBilling(ctx, tx, cmd.UserID); existsErr != nil {
		return nil, existsErr
	} else if !exists {
		return nil, service.ErrUserNotFound
	}
	return nil, service.ErrBatchImageInsufficientBalance
}

func captureUsageBillingBatchImageBalance(ctx context.Context, tx *sql.Tx, cmd *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error) {
	if cmd.HoldAmount <= 0 && cmd.ActualAmount <= 0 {
		return &service.BatchImageBalanceHoldResult{}, nil
	}
	if cmd.ActualAmount-cmd.HoldAmount > 0.00000001 {
		return nil, service.ErrBatchImageSettlementCostExceedsHold
	}
	var balance, frozen float64
	err := tx.QueryRowContext(ctx, `
		UPDATE users
		SET balance = balance
				+ CASE WHEN $1 > $2 THEN $1 - $2 ELSE 0 END
				- CASE WHEN $2 > $1 THEN $2 - $1 ELSE 0 END,
			frozen_balance = COALESCE(frozen_balance, 0) - $1,
			updated_at = NOW()
		WHERE id = $3 AND deleted_at IS NULL AND COALESCE(frozen_balance, 0) >= $1
		RETURNING balance, frozen_balance
	`, cmd.HoldAmount, cmd.ActualAmount, cmd.UserID).Scan(&balance, &frozen)
	if err == nil {
		return &service.BatchImageBalanceHoldResult{NewBalance: &balance, FrozenBalance: &frozen}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if exists, existsErr := userExistsForBilling(ctx, tx, cmd.UserID); existsErr != nil {
		return nil, existsErr
	} else if !exists {
		return nil, service.ErrUserNotFound
	}
	return nil, errors.New("batch image frozen balance is insufficient")
}

func releaseUsageBillingBatchImageBalance(ctx context.Context, tx *sql.Tx, cmd *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error) {
	if cmd.HoldAmount <= 0 {
		return &service.BatchImageBalanceHoldResult{}, nil
	}
	// 释放前校验该 job 确实预留过 hold（hold request id 已被 claim），
	// 防止从未成功冻结的 job 触发"幻影释放"，从其他用户的冻结资金池中凭空生成余额。
	held, heldErr := batchImageHoldClaimExists(ctx, tx, service.BatchImageHoldRequestID(cmd.BatchID), cmd.APIKeyID)
	if heldErr != nil {
		return nil, heldErr
	}
	if !held {
		logger.LegacyPrintf("repository.usage_billing", "[BatchImage] release skipped, hold was never reserved: batch=%s", cmd.BatchID)
		return &service.BatchImageBalanceHoldResult{}, nil
	}
	var balance, frozen float64
	err := tx.QueryRowContext(ctx, `
		UPDATE users
		SET balance = balance + $1,
			frozen_balance = COALESCE(frozen_balance, 0) - $1,
			updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL AND COALESCE(frozen_balance, 0) >= $1
		RETURNING balance, frozen_balance
	`, cmd.HoldAmount, cmd.UserID).Scan(&balance, &frozen)
	if err == nil {
		return &service.BatchImageBalanceHoldResult{NewBalance: &balance, FrozenBalance: &frozen}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if exists, existsErr := userExistsForBilling(ctx, tx, cmd.UserID); existsErr != nil {
		return nil, existsErr
	} else if !exists {
		return nil, service.ErrUserNotFound
	}
	return nil, errors.New("batch image frozen balance is insufficient")
}

// batchImageHoldClaimExists 检查 hold request id 是否已在 dedup（或归档）表中被 claim，
// 即该 batch 的冻结操作确实成功提交过。
func batchImageHoldClaimExists(ctx context.Context, tx *sql.Tx, holdRequestID string, apiKeyID int64) (bool, error) {
	var exists int
	err := tx.QueryRowContext(ctx, `
		SELECT 1
		FROM usage_billing_dedup
		WHERE request_id = $1 AND api_key_id = $2
	`, holdRequestID, apiKeyID).Scan(&exists)
	if err == nil {
		return true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	err = tx.QueryRowContext(ctx, `
		SELECT 1
		FROM usage_billing_dedup_archive
		WHERE request_id = $1 AND api_key_id = $2
	`, holdRequestID, apiKeyID).Scan(&exists)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return false, err
}

func userExistsForBilling(ctx context.Context, tx *sql.Tx, userID int64) (bool, error) {
	var exists int
	err := tx.QueryRowContext(ctx, `
		SELECT 1
		FROM users
		WHERE id = $1 AND deleted_at IS NULL
	`, userID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func incrementUsageBillingAPIKeyQuota(ctx context.Context, tx *sql.Tx, apiKeyID int64, amount float64) (bool, error) {
	var exhausted bool
	err := tx.QueryRowContext(ctx, `
		UPDATE api_keys
		SET quota_used = quota_used + $1,
			status = CASE
				WHEN quota > 0
					AND status = $3
					AND quota_used < quota
					AND quota_used + $1 >= quota
				THEN $4
				ELSE status
			END,
			updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL
		RETURNING quota > 0 AND quota_used >= quota AND quota_used - $1 < quota
	`, amount, apiKeyID, service.StatusAPIKeyActive, service.StatusAPIKeyQuotaExhausted).Scan(&exhausted)
	if errors.Is(err, sql.ErrNoRows) {
		return false, service.ErrAPIKeyNotFound
	}
	if err != nil {
		return false, err
	}
	return exhausted, nil
}

func incrementUsageBillingAPIKeyRateLimit(ctx context.Context, tx *sql.Tx, apiKeyID int64, cost float64) error {
	res, err := tx.ExecContext(ctx, `
		UPDATE api_keys SET
			usage_5h = CASE WHEN window_5h_start IS NOT NULL AND window_5h_start + INTERVAL '5 hours' <= NOW() THEN $1 ELSE usage_5h + $1 END,
			usage_1d = CASE WHEN window_1d_start IS NOT NULL AND window_1d_start + INTERVAL '24 hours' <= NOW() THEN $1 ELSE usage_1d + $1 END,
			usage_7d = CASE
				WHEN EXISTS (SELECT 1 FROM enterprise_key_assignments assignment WHERE assignment.api_key_id = api_keys.id AND assignment.status = 'active') THEN usage_7d + $1
				WHEN window_7d_start IS NOT NULL AND window_7d_start + INTERVAL '7 days' <= NOW() THEN $1 ELSE usage_7d + $1 END,
			window_5h_start = CASE WHEN window_5h_start IS NULL OR window_5h_start + INTERVAL '5 hours' <= NOW() THEN NOW() ELSE window_5h_start END,
			window_1d_start = CASE WHEN window_1d_start IS NULL OR window_1d_start + INTERVAL '24 hours' <= NOW() THEN date_trunc('day', NOW()) ELSE window_1d_start END,
			window_7d_start = CASE
				WHEN EXISTS (SELECT 1 FROM enterprise_key_assignments assignment WHERE assignment.api_key_id = api_keys.id AND assignment.status = 'active') THEN window_7d_start
				WHEN window_7d_start IS NULL OR window_7d_start + INTERVAL '7 days' <= NOW() THEN date_trunc('day', NOW()) ELSE window_7d_start END,
			updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL
	`, cost, apiKeyID)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return service.ErrAPIKeyNotFound
	}
	return nil
}

func incrementUsageBillingAccountQuota(ctx context.Context, tx *sql.Tx, accountID int64, amount float64) (*service.AccountQuotaState, error) {
	rows, err := tx.QueryContext(ctx,
		`UPDATE accounts SET extra = (
			COALESCE(extra, '{}'::jsonb)
			|| jsonb_build_object('quota_used', COALESCE((extra->>'quota_used')::numeric, 0) + $1)
			|| CASE WHEN COALESCE((extra->>'quota_daily_limit')::numeric, 0) > 0 THEN
				jsonb_build_object(
					'quota_daily_used',
					CASE WHEN `+dailyExpiredExpr+`
					THEN $1
					ELSE COALESCE((extra->>'quota_daily_used')::numeric, 0) + $1 END,
					'quota_daily_start',
					CASE WHEN `+dailyExpiredExpr+`
					THEN `+nowUTC+`
					ELSE COALESCE(extra->>'quota_daily_start', `+nowUTC+`) END
				)
				|| CASE WHEN `+dailyExpiredExpr+` AND `+nextDailyResetAtExpr+` IS NOT NULL
				   THEN jsonb_build_object('quota_daily_reset_at', `+nextDailyResetAtExpr+`)
				   ELSE '{}'::jsonb END
			ELSE '{}'::jsonb END
			|| CASE WHEN COALESCE((extra->>'quota_weekly_limit')::numeric, 0) > 0 THEN
				jsonb_build_object(
					'quota_weekly_used',
					CASE WHEN `+weeklyExpiredExpr+`
					THEN $1
					ELSE COALESCE((extra->>'quota_weekly_used')::numeric, 0) + $1 END,
					'quota_weekly_start',
					CASE WHEN `+weeklyExpiredExpr+`
					THEN `+nowUTC+`
					ELSE COALESCE(extra->>'quota_weekly_start', `+nowUTC+`) END
				)
				|| CASE WHEN `+weeklyExpiredExpr+` AND `+nextWeeklyResetAtExpr+` IS NOT NULL
				   THEN jsonb_build_object('quota_weekly_reset_at', `+nextWeeklyResetAtExpr+`)
				   ELSE '{}'::jsonb END
			ELSE '{}'::jsonb END
		), updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL
		RETURNING
			COALESCE((extra->>'quota_used')::numeric, 0),
			COALESCE((extra->>'quota_limit')::numeric, 0),
			COALESCE((extra->>'quota_daily_used')::numeric, 0),
			COALESCE((extra->>'quota_daily_limit')::numeric, 0),
			COALESCE((extra->>'quota_weekly_used')::numeric, 0),
			COALESCE((extra->>'quota_weekly_limit')::numeric, 0)`,
		amount, accountID)
	if err != nil {
		return nil, err
	}

	var state service.AccountQuotaState
	if rows.Next() {
		if err := rows.Scan(
			&state.TotalUsed, &state.TotalLimit,
			&state.DailyUsed, &state.DailyLimit,
			&state.WeeklyUsed, &state.WeeklyLimit,
		); err != nil {
			_ = rows.Close()
			return nil, err
		}
	} else {
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		_ = rows.Close()
		return nil, service.ErrAccountNotFound
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	// 必须在执行下一条 SQL 前显式关闭 rows：pq 驱动在同一连接上
	// 不允许前一条查询的结果集未耗尽时启动新查询，否则会返回
	// "unexpected Parse response" 错误。
	if err := rows.Close(); err != nil {
		return nil, err
	}
	// 任意维度额度在本次递增中从"未超"跨越到"已超"时，必须刷新调度快照，
	// 否则 Redis 中缓存的 Account 仍显示旧的 used 值，后续请求会继续选中本账号，
	// 最终观察到 daily_used / weekly_used 大幅超过配置的 limit。
	// 对于日/周额度，即使本次触发了周期重置（pre=0、post=amount），
	// 判定式 (post-amount) < limit 同样成立，逻辑与总额度保持一致。
	crossedTotal := state.TotalLimit > 0 && state.TotalUsed >= state.TotalLimit && (state.TotalUsed-amount) < state.TotalLimit
	crossedDaily := state.DailyLimit > 0 && state.DailyUsed >= state.DailyLimit && (state.DailyUsed-amount) < state.DailyLimit
	crossedWeekly := state.WeeklyLimit > 0 && state.WeeklyUsed >= state.WeeklyLimit && (state.WeeklyUsed-amount) < state.WeeklyLimit
	if crossedTotal || crossedDaily || crossedWeekly {
		if err := enqueueSchedulerOutbox(ctx, tx, service.SchedulerOutboxEventAccountChanged, &accountID, nil, nil); err != nil {
			logger.LegacyPrintf("repository.usage_billing", "[SchedulerOutbox] enqueue quota exceeded failed: account=%d err=%v", accountID, err)
			return nil, err
		}
	}
	return &state, nil
}
