package enterprise

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var (
	ErrEmployeeKeyNotFound        = infraerrors.NotFound("ENTERPRISE_KEY_NOT_FOUND", "enterprise employee key not found")
	ErrEmployeeKeyAlreadyActive   = infraerrors.Conflict("ENTERPRISE_KEY_ALREADY_ACTIVE", "enterprise employee already has an active key")
	ErrEmployeeKeyVersionConflict = infraerrors.Conflict("ENTERPRISE_KEY_VERSION_CONFLICT", "enterprise employee key changed; refresh and retry")
	ErrEmployeeKeyIdempotency     = infraerrors.Conflict("ENTERPRISE_KEY_IDEMPOTENCY_CONFLICT", "idempotency key was already used with a different request")
	ErrEmployeeKeyUnavailable     = infraerrors.New(http.StatusServiceUnavailable, "ENTERPRISE_KEY_UNAVAILABLE", "enterprise employee key service is unavailable")
	ErrEmployeeKeyProvisionLimit  = infraerrors.TooManyRequests("ENTERPRISE_KEY_PROVISION_LIMIT", "enterprise employee key provision limit reached")
)

const (
	keyOperationCreate        = "create"
	keyOperationDisable       = "disable"
	keyOperationRotate        = "rotate"
	employeeKeyProvisionLimit = 10
)

type EmployeeKey struct {
	ID          int64      `json:"id"`
	MaskedKey   string     `json:"masked_key"`
	Name        string     `json:"name"`
	Status      string     `json:"status"`
	Quota       float64    `json:"quota"`
	QuotaUsed   float64    `json:"quota_used"`
	GroupID     *int64     `json:"group_id,omitempty"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	RateLimit5h float64    `json:"rate_limit_5h"`
	RateLimit1d float64    `json:"rate_limit_1d"`
	RateLimit7d float64    `json:"rate_limit_7d"`
	Usage5h     float64    `json:"usage_5h"`
	Usage1d     float64    `json:"usage_1d"`
	Usage7d     float64    `json:"usage_7d"`
	Window5h    *time.Time `json:"window_5h_start,omitempty"`
	Window1d    *time.Time `json:"window_1d_start,omitempty"`
	Window7d    *time.Time `json:"window_7d_start,omitempty"`
	Reset5h     *time.Time `json:"reset_5h_at,omitempty"`
	Reset1d     *time.Time `json:"reset_1d_at,omitempty"`
	Reset7d     *time.Time `json:"reset_7d_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type EmployeeKeyMutationResult struct {
	Key       *EmployeeKey `json:"key"`
	Plaintext string       `json:"plaintext,omitempty"`
	Replayed  bool         `json:"replayed"`
}

type EmployeeKeyMutationParams struct {
	EnterpriseID     int64
	EmployeeID       int64
	ExpectedAPIKeyID int64
	IdempotencyKey   string
	Plaintext        string
	ActorRef         string
}

type EnterpriseKeySummary struct {
	APIKeyID   int64     `json:"api_key_id"`
	EmployeeID int64     `json:"employee_id"`
	Email      string    `json:"employee_email"`
	Generation int64     `json:"generation"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type employeeKeyRow struct {
	EmployeeKey
	Plaintext       string
	IPWhitelistJSON []byte
	IPBlacklistJSON []byte
	SubscriptionID  int64
	UpstreamGroupID int64
	Generation      int64
	AssignmentID    int64
}

func EmployeeKeyRequestHash(operation string, expectedAPIKeyID int64) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d", operation, expectedAPIKeyID)))
	return hex.EncodeToString(digest[:])
}

func EmployeeKeyCredentialFingerprint(credential string) string {
	digest := sha256.Sum256([]byte(credential))
	return hex.EncodeToString(digest[:])
}

func MaskEmployeeKey(value string) string {
	if len(value) <= 10 {
		return "********"
	}
	return value[:6] + "..." + value[len(value)-4:]
}

func (r *Repository) GetEmployeeCurrentKey(ctx context.Context, enterpriseID, employeeID int64) (*EmployeeKey, error) {
	row, err := scanEmployeeKey(r.db.QueryRowContext(ctx, employeeKeySelect+`
		WHERE assignment.enterprise_id = $1 AND assignment.employee_id = $2
		  AND assignment.status = 'active' AND employee.status = 'active'
		  AND api_key.deleted_at IS NULL
		ORDER BY assignment.generation DESC LIMIT 1`, enterpriseID, employeeID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row.EmployeeKey, nil
}

func (r *Repository) ListEnterpriseKeys(ctx context.Context, enterpriseID int64) ([]EnterpriseKeySummary, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT assignment.api_key_id, assignment.employee_id,
		       COALESCE(employee.current_email, employee.email, ''), assignment.generation,
		       CASE WHEN assignment.status = 'active' AND api_key.status = 'active' THEN 'active' ELSE 'disabled' END,
		       api_key.created_at, api_key.updated_at
		FROM enterprise_key_assignments AS assignment
		JOIN enterprise_employees AS employee
		  ON employee.enterprise_id = assignment.enterprise_id AND employee.id = assignment.employee_id
		JOIN api_keys AS api_key ON api_key.id = assignment.api_key_id
		WHERE assignment.enterprise_id = $1
		ORDER BY assignment.employee_id, assignment.generation DESC, assignment.api_key_id DESC`, enterpriseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]EnterpriseKeySummary, 0)
	for rows.Next() {
		var item EnterpriseKeySummary
		if err := rows.Scan(&item.APIKeyID, &item.EmployeeID, &item.Email, &item.Generation, &item.Status, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) RevokeEnterpriseKey(ctx context.Context, enterpriseID, apiKeyID int64, idempotencyKey, actorRef string) (*EmployeeKeyMutationResult, error) {
	var employeeID int64
	err := r.db.QueryRowContext(ctx, `
		SELECT employee_id FROM enterprise_key_assignments
		WHERE enterprise_id = $1 AND api_key_id = $2 AND status = 'active'`, enterpriseID, apiKeyID).Scan(&employeeID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrEmployeeKeyNotFound
	}
	if err != nil {
		return nil, err
	}
	return r.DisableEmployeeKey(ctx, EmployeeKeyMutationParams{
		EnterpriseID: enterpriseID, EmployeeID: employeeID, ExpectedAPIKeyID: apiKeyID,
		IdempotencyKey: idempotencyKey, ActorRef: actorRef,
	})
}

func (r *Repository) CreateEmployeeKey(ctx context.Context, params EmployeeKeyMutationParams) (*EmployeeKeyMutationResult, error) {
	return r.mutateEmployeeKey(ctx, keyOperationCreate, params)
}

func (r *Repository) DisableEmployeeKey(ctx context.Context, params EmployeeKeyMutationParams) (*EmployeeKeyMutationResult, error) {
	return r.mutateEmployeeKey(ctx, keyOperationDisable, params)
}

func (r *Repository) RotateEmployeeKey(ctx context.Context, params EmployeeKeyMutationParams) (*EmployeeKeyMutationResult, error) {
	return r.mutateEmployeeKey(ctx, keyOperationRotate, params)
}

func (r *Repository) mutateEmployeeKey(ctx context.Context, operation string, params EmployeeKeyMutationParams) (*EmployeeKeyMutationResult, error) {
	params.IdempotencyKey = strings.TrimSpace(params.IdempotencyKey)
	params.ActorRef = strings.TrimSpace(params.ActorRef)
	if params.EnterpriseID <= 0 || params.EmployeeID <= 0 || params.IdempotencyKey == "" || len(params.IdempotencyKey) > 128 || params.ActorRef == "" {
		return nil, infraerrors.BadRequest("INVALID_ENTERPRISE_KEY_REQUEST", "enterprise key request is invalid")
	}
	if (operation == keyOperationCreate || operation == keyOperationRotate) && params.Plaintext == "" {
		return nil, ErrEmployeeKeyUnavailable
	}
	requestHash := EmployeeKeyRequestHash(operation, params.ExpectedAPIKeyID)
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var dedicatedUserID int64
	if err = tx.QueryRowContext(ctx, `
		SELECT id FROM enterprise_employees
		WHERE enterprise_id = $1 AND id = $2 AND status = 'active'
		FOR UPDATE`, params.EnterpriseID, params.EmployeeID).Scan(new(int64)); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrEmployeeKeyNotFound
		}
		return nil, err
	}
	if err = tx.QueryRowContext(ctx, `
		SELECT dedicated_upstream_user_id FROM enterprises
		WHERE id = $1 AND status = 'active'
		FOR UPDATE`, params.EnterpriseID).Scan(&dedicatedUserID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrEmployeeKeyNotFound
		}
		return nil, err
	}

	replayedID, replayed, err := lockEmployeeKeyIdempotency(ctx, tx, params, operation, requestHash)
	if err != nil {
		return nil, err
	}
	if replayed {
		row, loadErr := loadEmployeeKeyByID(ctx, tx, params.EnterpriseID, params.EmployeeID, replayedID)
		if loadErr != nil {
			return nil, loadErr
		}
		if err = tx.Commit(); err != nil {
			return nil, err
		}
		return &EmployeeKeyMutationResult{Key: &row.EmployeeKey, Replayed: true}, nil
	}

	if operation == keyOperationCreate || operation == keyOperationRotate {
		if err := enforceEmployeeKeyProvisionLimit(ctx, tx, params.EnterpriseID, params.EmployeeID); err != nil {
			return nil, err
		}
	}

	current, err := readCurrentEmployeeKey(ctx, tx, params.EnterpriseID, params.EmployeeID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	hasCurrent := err == nil
	previous := current
	if operation == keyOperationCreate {
		if hasCurrent {
			return nil, ErrEmployeeKeyAlreadyActive
		}
		previous, err = lockLatestEmployeeKey(ctx, tx, params.EnterpriseID, params.EmployeeID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		if errors.Is(err, sql.ErrNoRows) {
			previous = employeeKeyRow{}
		}
	} else {
		if !hasCurrent {
			return nil, ErrEmployeeKeyNotFound
		}
		if params.ExpectedAPIKeyID <= 0 || current.ID != params.ExpectedAPIKeyID {
			return nil, ErrEmployeeKeyVersionConflict
		}
		if err := lockEmployeeKeyCredential(ctx, tx, current.Plaintext); err != nil {
			return nil, err
		}
		current, err = lockCurrentEmployeeKey(ctx, tx, params.EnterpriseID, params.EmployeeID)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrEmployeeKeyVersionConflict
		}
		if err != nil {
			return nil, err
		}
		if current.ID != params.ExpectedAPIKeyID {
			return nil, ErrEmployeeKeyVersionConflict
		}
	}

	var result employeeKeyRow
	var oldPlaintext string
	switch operation {
	case keyOperationCreate:
		if previous.ID == 0 {
			result, err = insertFirstEmployeeKey(ctx, tx, params, dedicatedUserID)
			break
		}
		boundary, boundaryErr := employeeKeyBoundary(ctx, tx)
		if boundaryErr != nil {
			return nil, boundaryErr
		}
		result, err = insertSuccessorEmployeeKey(ctx, tx, params, dedicatedUserID, previous, boundary)
	case keyOperationDisable:
		boundary, boundaryErr := employeeKeyBoundary(ctx, tx)
		if boundaryErr != nil {
			return nil, boundaryErr
		}
		result = current
		oldPlaintext = current.Plaintext
		err = disableCurrentEmployeeKey(ctx, tx, params, current, boundary)
		result.Status = "disabled"
		result.UpdatedAt = boundary
	case keyOperationRotate:
		boundary, boundaryErr := employeeKeyBoundary(ctx, tx)
		if boundaryErr != nil {
			return nil, boundaryErr
		}
		oldPlaintext = current.Plaintext
		result, err = rotateCurrentEmployeeKey(ctx, tx, params, dedicatedUserID, current, boundary)
	default:
		return nil, infraerrors.BadRequest("INVALID_ENTERPRISE_KEY_OPERATION", "enterprise key operation is invalid")
	}
	if err != nil {
		return nil, err
	}
	if err = writeEmployeeKeyAudit(ctx, tx, params, operation, previous, result); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `
		UPDATE enterprise_key_lifecycle_idempotency
		SET result_api_key_id = $5, completed_at = clock_timestamp()
		WHERE enterprise_id = $1 AND employee_id = $2 AND operation = $3
		  AND idempotency_key = $4 AND result_api_key_id IS NULL`,
		params.EnterpriseID, params.EmployeeID, operation, params.IdempotencyKey, result.ID); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	if oldPlaintext != "" {
		r.authCacheInvalidator.InvalidateAuthCacheByKey(ctx, oldPlaintext)
	}
	plaintext := ""
	if operation == keyOperationCreate || operation == keyOperationRotate {
		plaintext = params.Plaintext
	}
	return &EmployeeKeyMutationResult{Key: &result.EmployeeKey, Plaintext: plaintext}, nil
}

func lockEmployeeKeyIdempotency(ctx context.Context, tx *sql.Tx, params EmployeeKeyMutationParams, operation, requestHash string) (int64, bool, error) {
	result, err := tx.ExecContext(ctx, `
		INSERT INTO enterprise_key_lifecycle_idempotency (
			enterprise_id, employee_id, operation, idempotency_key, request_hash
		) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (enterprise_id, employee_id, operation, idempotency_key) DO NOTHING`,
		params.EnterpriseID, params.EmployeeID, operation, params.IdempotencyKey, requestHash)
	if err != nil {
		return 0, false, err
	}
	affected, err := result.RowsAffected()
	if err != nil || affected == 1 {
		return 0, false, err
	}
	var storedHash string
	var resultID sql.NullInt64
	if err = tx.QueryRowContext(ctx, `
		SELECT request_hash, result_api_key_id
		FROM enterprise_key_lifecycle_idempotency
		WHERE enterprise_id = $1 AND employee_id = $2 AND operation = $3 AND idempotency_key = $4
		FOR UPDATE`, params.EnterpriseID, params.EmployeeID, operation, params.IdempotencyKey).Scan(&storedHash, &resultID); err != nil {
		return 0, false, err
	}
	if storedHash != requestHash {
		return 0, false, ErrEmployeeKeyIdempotency
	}
	if !resultID.Valid {
		return 0, false, ErrEmployeeKeyUnavailable
	}
	return resultID.Int64, true, nil
}

func enforceEmployeeKeyProvisionLimit(ctx context.Context, tx *sql.Tx, enterpriseID, employeeID int64) error {
	var count int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM enterprise_key_lifecycle_idempotency
		WHERE enterprise_id = $1
		  AND employee_id = $2
		  AND operation IN ('create', 'rotate')
		  AND completed_at IS NOT NULL
		  AND completed_at >= statement_timestamp() - INTERVAL '24 hours'`, enterpriseID, employeeID).Scan(&count); err != nil {
		return err
	}
	if count >= employeeKeyProvisionLimit {
		return ErrEmployeeKeyProvisionLimit
	}
	return nil
}

const employeeKeySelect = `
	SELECT api_key.id, api_key.key, api_key.name, api_key.status,
	       api_key.quota, api_key.quota_used, api_key.group_id, api_key.expires_at,
	       api_key.rate_limit_5h, api_key.rate_limit_1d, api_key.rate_limit_7d,
	       api_key.usage_5h, api_key.usage_1d, api_key.usage_7d,
	       api_key.window_5h_start, api_key.window_1d_start, api_key.window_7d_start,
	       api_key.ip_whitelist, api_key.ip_blacklist,
	       api_key.created_at, api_key.updated_at,
	       assignment.upstream_user_subscription_id, assignment.upstream_group_id, assignment.generation, assignment.id
	FROM enterprise_key_assignments AS assignment
	JOIN enterprise_employees AS employee
	  ON employee.enterprise_id = assignment.enterprise_id AND employee.id = assignment.employee_id
	JOIN api_keys AS api_key ON api_key.id = assignment.api_key_id`

func lockCurrentEmployeeKey(ctx context.Context, tx *sql.Tx, enterpriseID, employeeID int64) (employeeKeyRow, error) {
	return scanEmployeeKey(tx.QueryRowContext(ctx, employeeKeySelect+`
		WHERE assignment.enterprise_id = $1 AND assignment.employee_id = $2
		  AND assignment.status = 'active' AND api_key.deleted_at IS NULL
		ORDER BY assignment.generation DESC, assignment.assigned_at DESC, assignment.id DESC LIMIT 1
		FOR UPDATE OF assignment, api_key`, enterpriseID, employeeID))
}

func readCurrentEmployeeKey(ctx context.Context, tx *sql.Tx, enterpriseID, employeeID int64) (employeeKeyRow, error) {
	return scanEmployeeKey(tx.QueryRowContext(ctx, employeeKeySelect+`
		WHERE assignment.enterprise_id = $1 AND assignment.employee_id = $2
		  AND assignment.status = 'active' AND api_key.deleted_at IS NULL
		ORDER BY assignment.generation DESC, assignment.assigned_at DESC, assignment.id DESC LIMIT 1`, enterpriseID, employeeID))
}

func lockEmployeeKeyCredential(ctx context.Context, tx *sql.Tx, credential string) error {
	if credential == "" {
		return ErrEmployeeKeyVersionConflict
	}
	fingerprint := EmployeeKeyCredentialFingerprint(credential)
	_, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, fingerprint)
	return err
}

func lockLatestEmployeeKey(ctx context.Context, tx *sql.Tx, enterpriseID, employeeID int64) (employeeKeyRow, error) {
	return scanEmployeeKey(tx.QueryRowContext(ctx, employeeKeySelect+`
		WHERE assignment.enterprise_id = $1 AND assignment.employee_id = $2
		  AND assignment.status <> 'active'
		ORDER BY assignment.generation DESC, assignment.assigned_at DESC, assignment.id DESC LIMIT 1
		FOR UPDATE OF assignment, api_key`, enterpriseID, employeeID))
}

func loadEmployeeKeyByID(ctx context.Context, tx *sql.Tx, enterpriseID, employeeID, apiKeyID int64) (employeeKeyRow, error) {
	return scanEmployeeKey(tx.QueryRowContext(ctx, employeeKeySelect+`
		WHERE assignment.enterprise_id = $1 AND assignment.employee_id = $2 AND api_key.id = $3
		ORDER BY assignment.generation DESC, assignment.assigned_at DESC, assignment.id DESC LIMIT 1`, enterpriseID, employeeID, apiKeyID))
}

func scanEmployeeKey(row *sql.Row) (employeeKeyRow, error) {
	var result employeeKeyRow
	var groupID sql.NullInt64
	var expiresAt, window5h, window1d, window7d sql.NullTime
	err := row.Scan(&result.ID, &result.Plaintext, &result.Name, &result.Status,
		&result.Quota, &result.QuotaUsed, &groupID, &expiresAt,
		&result.RateLimit5h, &result.RateLimit1d, &result.RateLimit7d,
		&result.Usage5h, &result.Usage1d, &result.Usage7d,
		&window5h, &window1d, &window7d, &result.IPWhitelistJSON, &result.IPBlacklistJSON,
		&result.CreatedAt, &result.UpdatedAt, &result.SubscriptionID, &result.UpstreamGroupID, &result.Generation, &result.AssignmentID)
	if err != nil {
		return result, err
	}
	result.GroupID = nullInt64Pointer(groupID)
	result.ExpiresAt = nullTimePointer(expiresAt)
	result.Window5h = nullTimePointer(window5h)
	result.Window1d = nullTimePointer(window1d)
	result.Window7d = nullTimePointer(window7d)
	normalizeEmployeeKeyPresentation(&result.EmployeeKey, time.Now())
	result.MaskedKey = MaskEmployeeKey(result.Plaintext)
	return result, nil
}

func normalizeEmployeeKeyPresentation(key *EmployeeKey, now time.Time) {
	if key.ExpiresAt != nil && !key.ExpiresAt.After(now) && key.Status != "disabled" {
		key.Status = "expired"
	} else if key.Status == "active" && key.Quota > 0 && key.QuotaUsed >= key.Quota {
		key.Status = "quota_exhausted"
	}
	key.Usage5h, key.Window5h, key.Reset5h = normalizeEmployeeKeyWindow(key.Usage5h, key.Window5h, 5*time.Hour, now)
	key.Usage1d, key.Window1d, key.Reset1d = normalizeEmployeeKeyWindow(key.Usage1d, key.Window1d, 24*time.Hour, now)
	key.Usage7d, key.Window7d, key.Reset7d = normalizeEmployeeKeyWindow(key.Usage7d, key.Window7d, 7*24*time.Hour, now)
}

func normalizeEmployeeKeyWindow(usage float64, start *time.Time, duration time.Duration, now time.Time) (float64, *time.Time, *time.Time) {
	if start == nil || !start.Add(duration).After(now) {
		return 0, nil, nil
	}
	reset := start.Add(duration)
	return usage, start, &reset
}

func insertFirstEmployeeKey(ctx context.Context, tx *sql.Tx, params EmployeeKeyMutationParams, dedicatedUserID int64) (employeeKeyRow, error) {
	subscriptionID, groupID, err := lockEmployeeKeySubscription(ctx, tx, params.EnterpriseID, dedicatedUserID)
	if err != nil {
		return employeeKeyRow{}, err
	}
	var keyID int64
	err = tx.QueryRowContext(ctx, `
		INSERT INTO api_keys (user_id, key, name, group_id, status)
		VALUES ($1, $2, 'Enterprise employee key', $3, 'active') RETURNING id`,
		dedicatedUserID, params.Plaintext, groupID).Scan(&keyID)
	if err != nil {
		return employeeKeyRow{}, err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO enterprise_key_assignments (
			enterprise_id, employee_id, api_key_id, upstream_user_subscription_id,
			upstream_group_id, generation, status, actor_ref
		) VALUES ($1, $2, $3, $4, $5,
			COALESCE((SELECT MAX(generation) + 1 FROM enterprise_key_assignments WHERE employee_id = $2), 1),
			'active', $6)`, params.EnterpriseID, params.EmployeeID, keyID, subscriptionID, groupID, params.ActorRef)
	if err != nil {
		return employeeKeyRow{}, err
	}
	return loadEmployeeKeyByID(ctx, tx, params.EnterpriseID, params.EmployeeID, keyID)
}

func lockEmployeeKeySubscription(ctx context.Context, tx *sql.Tx, enterpriseID, dedicatedUserID int64) (int64, int64, error) {
	var subscriptionID, groupID int64
	var upstreamStatus string
	var startsAt, expiresAt time.Time
	if err := tx.QueryRowContext(ctx, `
		SELECT enterprise_subscription.upstream_user_subscription_id, upstream_subscription.group_id,
		       upstream_subscription.status, upstream_subscription.starts_at, upstream_subscription.expires_at
		FROM enterprise_subscriptions AS enterprise_subscription
		JOIN user_subscriptions AS upstream_subscription
		  ON upstream_subscription.id = enterprise_subscription.upstream_user_subscription_id
		 AND upstream_subscription.user_id = $2 AND upstream_subscription.deleted_at IS NULL
		WHERE enterprise_subscription.enterprise_id = $1 AND enterprise_subscription.status = 'active'
		FOR UPDATE OF enterprise_subscription, upstream_subscription`, enterpriseID, dedicatedUserID).Scan(&subscriptionID, &groupID, &upstreamStatus, &startsAt, &expiresAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, 0, ErrEmployeeKeyUnavailable
		}
		return 0, 0, err
	}
	var now time.Time
	if err := tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return 0, 0, err
	}
	if upstreamStatus != "active" || now.Before(startsAt) || !expiresAt.After(now) {
		return 0, 0, ErrEmployeeKeyUnavailable
	}
	return subscriptionID, groupID, nil
}

func employeeKeyBoundary(ctx context.Context, tx *sql.Tx) (time.Time, error) {
	var boundary time.Time
	err := tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&boundary)
	return boundary, err
}

func disableCurrentEmployeeKey(ctx context.Context, tx *sql.Tx, params EmployeeKeyMutationParams, current employeeKeyRow, boundary time.Time) error {
	result, err := tx.ExecContext(ctx, `
		UPDATE enterprise_key_assignments
		SET status = 'revoked', ended_at = $4, revoked_at = $4, actor_ref = $3, updated_at = $4
		WHERE enterprise_id = $1 AND employee_id = $2 AND api_key_id = $5 AND status = 'active'`,
		params.EnterpriseID, params.EmployeeID, params.ActorRef, boundary, current.ID)
	if err != nil {
		return err
	}
	if affected, rowsErr := result.RowsAffected(); rowsErr != nil || affected != 1 {
		if rowsErr != nil {
			return rowsErr
		}
		return ErrEmployeeKeyVersionConflict
	}
	result, err = tx.ExecContext(ctx, `UPDATE api_keys SET key = ':revoked:' || id::text, status = 'disabled', updated_at = $2 WHERE id = $1`, current.ID, boundary)
	if err != nil {
		return err
	}
	if affected, rowsErr := result.RowsAffected(); rowsErr != nil || affected != 1 {
		if rowsErr != nil {
			return rowsErr
		}
		return ErrEmployeeKeyVersionConflict
	}
	return nil
}

func rotateCurrentEmployeeKey(ctx context.Context, tx *sql.Tx, params EmployeeKeyMutationParams, dedicatedUserID int64, current employeeKeyRow, boundary time.Time) (employeeKeyRow, error) {
	if err := disableCurrentEmployeeKey(ctx, tx, params, current, boundary); err != nil {
		return employeeKeyRow{}, err
	}
	return insertSuccessorEmployeeKey(ctx, tx, params, dedicatedUserID, current, boundary)
}

func insertSuccessorEmployeeKey(ctx context.Context, tx *sql.Tx, params EmployeeKeyMutationParams, dedicatedUserID int64, previous employeeKeyRow, boundary time.Time) (employeeKeyRow, error) {
	subscriptionID, groupID, err := lockEmployeeKeySubscription(ctx, tx, params.EnterpriseID, dedicatedUserID)
	if err != nil {
		return employeeKeyRow{}, err
	}
	var keyID int64
	err = tx.QueryRowContext(ctx, `
		INSERT INTO api_keys (
			user_id, key, name, group_id, status, ip_whitelist, ip_blacklist,
			quota, quota_used, expires_at, rate_limit_5h, rate_limit_1d, rate_limit_7d,
			usage_5h, usage_1d, usage_7d, window_5h_start, window_1d_start, window_7d_start
		)
		SELECT $1, $2, source.name, $3,
		       CASE
		           WHEN source.expires_at IS NOT NULL AND source.expires_at <= $5 THEN 'expired'
		           WHEN source.quota > 0 AND source.quota_used >= source.quota THEN 'quota_exhausted'
		           ELSE 'active'
		       END,
		       source.ip_whitelist, source.ip_blacklist,
		       source.quota, source.quota_used, source.expires_at,
		       source.rate_limit_5h, source.rate_limit_1d, source.rate_limit_7d,
		       source.usage_5h, source.usage_1d, source.usage_7d,
		       source.window_5h_start, source.window_1d_start, source.window_7d_start
		FROM api_keys AS source
		WHERE source.id = $4
		RETURNING id`, dedicatedUserID, params.Plaintext, groupID, previous.ID, boundary).Scan(&keyID)
	if err != nil {
		return employeeKeyRow{}, err
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO enterprise_key_assignments (
			enterprise_id, employee_id, api_key_id, upstream_user_subscription_id,
			upstream_group_id, generation, status, assigned_at, actor_ref, created_at, updated_at
		)
		SELECT $1, $2, $3, $4, $5, source.generation + 1, 'active', $7, $6, $7, $7
		FROM enterprise_key_assignments AS source
		WHERE source.id = $8 AND source.enterprise_id = $1 AND source.employee_id = $2
		  AND source.api_key_id = $9`,
		params.EnterpriseID, params.EmployeeID, keyID, subscriptionID, groupID, params.ActorRef, boundary, previous.AssignmentID, previous.ID)
	if err != nil {
		return employeeKeyRow{}, err
	}
	if affected, rowsErr := result.RowsAffected(); rowsErr != nil || affected != 1 {
		if rowsErr != nil {
			return employeeKeyRow{}, rowsErr
		}
		return employeeKeyRow{}, ErrEmployeeKeyVersionConflict
	}
	return loadEmployeeKeyByID(ctx, tx, params.EnterpriseID, params.EmployeeID, keyID)
}

func nullableJSONParameter(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return string(value)
}

func writeEmployeeKeyAudit(ctx context.Context, tx *sql.Tx, params EmployeeKeyMutationParams, operation string, previous, result employeeKeyRow) error {
	payload := map[string]any{
		"employee_id": params.EmployeeID, "api_key_id": result.ID, "masked_key": result.MaskedKey,
		"quota": result.Quota, "quota_used": result.QuotaUsed,
		"rate_limit_5h": result.RateLimit5h, "rate_limit_1d": result.RateLimit1d, "rate_limit_7d": result.RateLimit7d,
		"usage_5h": result.Usage5h, "usage_1d": result.Usage1d, "usage_7d": result.Usage7d,
		"window_5h_start": result.Window5h, "window_1d_start": result.Window1d, "window_7d_start": result.Window7d,
	}
	if previous.ID > 0 && previous.ID != result.ID {
		payload["previous_api_key_id"] = previous.ID
		payload["previous_masked_key"] = previous.MaskedKey
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO enterprise_audit_events (enterprise_id, event_type, entity_type, entity_id, payload, actor_ref)
		VALUES ($1, $2, 'api_key', $3, $4::jsonb, $5)`,
		params.EnterpriseID, "key.employee_"+operation, result.ID, raw, params.ActorRef)
	return err
}

func addDuration(start *time.Time, duration time.Duration) *time.Time {
	if start == nil {
		return nil
	}
	result := start.Add(duration)
	return &result
}
