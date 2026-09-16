package enterpriseidentity

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	_ "golang.org/x/image/webp"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/enterprise"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	platformservice "github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

const (
	maxBrandBackgroundBytes                 = 5 * 1024 * 1024
	maxBrandImageDimension                  = 8192
	maxBrandImagePixels                     = 16 * 1024 * 1024
	accessTokenTTL                          = 15 * time.Minute
	refreshTokenTTL                         = 30 * 24 * time.Hour
	passwordResetTTL                        = time.Hour
	defaultBrandEnterpriseName              = "Sub2API"
	defaultBrandTitle                       = "企业工作台"
	defaultBrandBody                        = "使用企业管理员或员工账号安全访问组织资源。"
	defaultBrandSlogan                      = "安全、统一的企业访问入口"
	defaultBrandBackgroundURL               = "/logo.svg"
	defaultBrandBackgroundContentType       = "image/svg+xml"
	defaultBrandBackgroundSHA256            = "ce1f2ac07efcfff80904a9582578b5db8fdd14a3118a5c7b58f408ed06df18e1"
	defaultBrandBackgroundSize        int64 = 2010
	publicBrandBackgroundURL                = "/api/v1/enterprise/brand/background"
)

var (
	errInvalidCredentials        = infraerrors.Unauthorized("INVALID_CREDENTIALS", "invalid email or password")
	errInvalidToken              = infraerrors.Unauthorized("INVALID_ENTERPRISE_TOKEN", "invalid enterprise token")
	errEnterpriseInactive        = infraerrors.Unauthorized("ENTERPRISE_DISABLED", "enterprise workspace is not active")
	errPrincipalInactive         = infraerrors.Unauthorized("ENTERPRISE_PRINCIPAL_INACTIVE", "enterprise identity is not active")
	errInactive                  = errPrincipalInactive
	errWrongHost                 = infraerrors.Unauthorized("ENTERPRISE_HOST_MISMATCH", "enterprise token is not valid for this host")
	errPasswordExpired           = infraerrors.Forbidden("INITIAL_PASSWORD_EXPIRED", "initial password has expired")
	errForceChange               = infraerrors.Forbidden("PASSWORD_CHANGE_REQUIRED", "password must be changed before continuing")
	errResetInvalid              = infraerrors.BadRequest("PASSWORD_RESET_INVALID", "password reset token is invalid or expired")
	errDedicatedUserUnavailable  = infraerrors.BadRequest("DEDICATED_UPSTREAM_USER_UNAVAILABLE", "dedicated upstream user is not active")
	errSubscriptionUnavailable   = infraerrors.BadRequest("ENTERPRISE_SUBSCRIPTION_UNAVAILABLE", "dedicated upstream user has no active subscription")
	errNotFound                  = infraerrors.NotFound("ENTERPRISE_OBJECT_NOT_FOUND", "enterprise object not found")
	errEmployeeDepartmentInvalid = infraerrors.BadRequest("ENTERPRISE_EMPLOYEE_DEPARTMENT_INVALID", "selected department is invalid")
	errConflict                  = infraerrors.Conflict("ENTERPRISE_CONFLICT", "enterprise object conflicts with an existing record")
	errInvalidBrand              = infraerrors.BadRequest("INVALID_ENTERPRISE_BRAND", "enterprise brand content is invalid")
	errEmployeeVersionConflict   = infraerrors.Conflict("EMPLOYEE_VERSION_CONFLICT", "employee was modified by another admin; reload and retry")
)

type PasswordResetMailer interface {
	SendEmail(ctx context.Context, to, subject, body string) error
}

type APIKeyAuthCacheInvalidator interface {
	InvalidateAuthCacheByKey(ctx context.Context, key string)
}

type BrandObjectStorage interface {
	Save(ctx context.Context, key, contentType string, data []byte) (string, error)
	Load(ctx context.Context, key string) ([]byte, string, error)
}

type BrandObject struct {
	URL         string `json:"background_url"`
	ContentType string `json:"background_content_type"`
	SHA256      string `json:"background_sha256"`
	Size        int64  `json:"background_size_bytes"`
}

type Service struct {
	db                   *sql.DB
	secret               []byte
	mailer               PasswordResetMailer
	authCacheInvalidator APIKeyAuthCacheInvalidator
	brandStorage         BrandObjectStorage
	brandStorageResolver func() (BrandObjectStorage, bool)
	now                  func() time.Time
}

type auditActorContextKey struct{}

// WithAuditActor 将已认证的企业操作者绑定到本次写请求。
func WithAuditActor(ctx context.Context, actorRef string) context.Context {
	return context.WithValue(ctx, auditActorContextKey{}, strings.TrimSpace(actorRef))
}

func auditActor(ctx context.Context) string {
	actor, _ := ctx.Value(auditActorContextKey{}).(string)
	return strings.TrimSpace(actor)
}

func writeAuditEvent(ctx context.Context, tx *sql.Tx, enterpriseID int64, eventType, entityType string, entityID *int64, payload any) error {
	actor := auditActor(ctx)
	if actor == "" {
		return nil
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO enterprise_audit_events (enterprise_id, event_type, entity_type, entity_id, payload, actor_ref)
		VALUES ($1, $2, $3, $4, $5::jsonb, $6)
	`, enterpriseID, eventType, entityType, entityID, encoded, actor)
	return err
}

// RecordRejectedAuditEvent 记录管理写入被拒绝的通用原因，不持久化原始请求正文。
func (s *Service) RecordRejectedAuditEvent(ctx context.Context, enterpriseID int64, eventType, entityType string, entityID *int64, reason string) error {
	actor := auditActor(ctx)
	if actor == "" || enterpriseID <= 0 {
		return nil
	}
	payload, err := json.Marshal(map[string]any{"result": "rejected", "reason": strings.TrimSpace(reason)})
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO enterprise_audit_events (enterprise_id, event_type, entity_type, entity_id, payload, actor_ref)
		VALUES ($1, $2, $3, $4, $5::jsonb, $6)
	`, enterpriseID, eventType, entityType, entityID, payload, actor)
	return err
}

func (s *Service) recordAuditEvent(ctx context.Context, enterpriseID int64, eventType, entityType string, entityID *int64, payload any) error {
	if auditActor(ctx) == "" || enterpriseID <= 0 {
		return nil
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO enterprise_audit_events (enterprise_id, event_type, entity_type, entity_id, payload, actor_ref)
		VALUES ($1, $2, $3, $4, $5::jsonb, $6)
	`, enterpriseID, eventType, entityType, entityID, encoded, auditActor(ctx))
	return err
}

type queryRower interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type dbExecutor interface {
	queryRower
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

type Enterprise struct {
	ID          int64
	Name        string
	Host        string
	AdminUserID int64
	Status      string
}

type PlatformEnterprise struct {
	ID                    int64                  `json:"id"`
	Name                  string                 `json:"name"`
	Host                  string                 `json:"portal_host"`
	DedicatedUpstreamUser int64                  `json:"dedicated_upstream_user_id"`
	Status                string                 `json:"status"`
	CreatedAt             time.Time              `json:"created_at"`
	AdminEmail            string                 `json:"admin_email"`
	EmployeeCount         int64                  `json:"employee_count"`
	ActiveEmployeeCount   int64                  `json:"active_employee_count"`
	ActiveSessionCount    int64                  `json:"active_session_count"`
	ActiveKeyCount        int64                  `json:"active_key_count"`
	Subscriptions         []PlatformSubscription `json:"subscriptions"`
}

type PlatformSubscription struct {
	ID                int64      `json:"id"`
	Status            string     `json:"status"`
	Plan              string     `json:"plan"`
	WeeklyLimit       string     `json:"weekly_limit"`
	WeeklyWindowStart *time.Time `json:"weekly_window_start,omitempty"`
	StartsAt          time.Time  `json:"starts_at"`
	ExpiresAt         time.Time  `json:"expires_at"`
}

type CreateEnterpriseInput struct {
	Name                  string
	Host                  string
	DedicatedUpstreamUser int64
	Reason                string
}

type Claims struct {
	EnterpriseID  int64  `json:"enterprise_id"`
	PrincipalType string `json:"principal_type"`
	PrincipalID   int64  `json:"principal_id"`
	AuthVersion   int64  `json:"auth_version"`
	Role          string `json:"role"`
	SessionID     string `json:"sid"`
	ForceChange   bool   `json:"force_password_change,omitempty"`
	jwt.RegisteredClaims
}

type Principal struct {
	EnterpriseID  int64  `json:"enterprise_id"`
	PrincipalType string `json:"principal_type"`
	PrincipalID   int64  `json:"principal_id"`
	Email         string `json:"email"`
	Role          string `json:"role"`
	ForceChange   bool   `json:"force_password_change"`
	AuthVersion   int64  `json:"-"`
}

type TokenPair struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	TokenType    string    `json:"token_type"`
	ExpiresIn    int       `json:"expires_in"`
	Principal    Principal `json:"principal"`
}

type Session struct {
	ID         string     `json:"id"`
	UserAgent  string     `json:"user_agent"`
	IPAddress  string     `json:"ip_address"`
	LastSeenAt time.Time  `json:"last_seen_at"`
	CreatedAt  time.Time  `json:"created_at"`
	ExpiresAt  time.Time  `json:"expires_at"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
	Current    bool       `json:"current"`
}

type Department struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// DepartmentDeletionImpact previews the effect of deleting a department so the
// admin can make an informed decision before the destructive action executes.
type DepartmentDeletionImpact struct {
	DepartmentID      int64  `json:"department_id"`
	DepartmentName    string `json:"department_name"`
	AffectedEmployees int64  `json:"affected_employees"`
}

type Employee struct {
	ID           int64      `json:"id"`
	Email        string     `json:"email"`
	Status       string     `json:"status"`
	DepartmentID *int64     `json:"department_id,omitempty"`
	MustChange   bool       `json:"must_change_password"`
	TerminatedAt *time.Time `json:"terminated_at,omitempty"`
	Version      int64      `json:"version"`
}

// EmployeeDetail extends Employee with the read-only context an admin needs
// on the employee detail page: join date, resolved department name, and the
// employee's current API key (already masked by the key repository — the
// plaintext credential never leaves the self-service key endpoints).
type EmployeeDetail struct {
	Employee
	DepartmentName *string                 `json:"department_name,omitempty"`
	CreatedAt      time.Time               `json:"created_at"`
	CurrentKey     *enterprise.EmployeeKey `json:"current_key,omitempty"`
}

type EmployeeUsageSummary struct {
	SubscriptionID *int64     `json:"subscription_id,omitempty"`
	WindowType     string     `json:"window_type"`
	WindowAnchor   *time.Time `json:"window_anchor,omitempty"`
	SourceStatus   string     `json:"source_status"`
	Allocation     string     `json:"allocation"`
	ActualCost     string     `json:"actual_cost"`
	Remaining      string     `json:"remaining"`
	Overage        string     `json:"overage"`
	Requests       int64      `json:"requests"`
}

type EmployeeUsageRecord struct {
	RequestAt    time.Time `json:"request_at"`
	WindowAnchor time.Time `json:"window_anchor"`
	APIKeyMasked string    `json:"api_key_masked"`
	Generation   int64     `json:"generation"`
	ActualCost   string    `json:"actual_cost"`
}

// EmployeeUsageQuery bounds ListEmployeeUsage to a time window and a page;
// StartAt/EndAt are optional and, when both set, StartAt must precede EndAt.
type EmployeeUsageQuery struct {
	StartAt  *time.Time
	EndAt    *time.Time
	Page     int
	PageSize int
}

type BrandInput struct {
	EnterpriseName        string `json:"enterprise_name"`
	Title                 string `json:"title"`
	Body                  string `json:"body"`
	Slogan                string `json:"slogan"`
	BackgroundObjectKey   string `json:"-"`
	BackgroundURL         string `json:"background_url"`
	BackgroundContentType string `json:"background_content_type"`
	BackgroundSHA256      string `json:"background_sha256"`
	BackgroundSize        int64  `json:"background_size_bytes"`
}

func NewService(db *sql.DB, cfg *config.Config, mailer *platformservice.EmailService, imageStorageSettings *platformservice.ImageStorageSettingService, authCacheInvalidator APIKeyAuthCacheInvalidator) *Service {
	secret := ""
	if cfg != nil {
		secret = cfg.JWT.Secret
	}
	service := &Service{db: db, secret: []byte(secret), authCacheInvalidator: authCacheInvalidator, now: time.Now}
	if mailer != nil {
		service.mailer = mailer
	}
	if imageStorageSettings != nil {
		service.brandStorageResolver = func() (BrandObjectStorage, bool) {
			storage, enabled := imageStorageSettings.ObjectStorage()
			readable, ok := storage.(BrandObjectStorage)
			return readable, enabled && ok
		}
	}
	return service
}

func requestHost(r *http.Request) string {
	host := strings.TrimSpace(strings.ToLower(r.Host))
	if parsed, _, err := net.SplitHostPort(host); err == nil {
		host = parsed
	}
	return strings.TrimSuffix(host, ".")
}

func (s *Service) enterpriseByHost(ctx context.Context, host string) (*Enterprise, error) {
	var e Enterprise
	err := s.db.QueryRowContext(ctx, `
		SELECT id, name, LOWER(BTRIM(portal_host)), admin_user_id, status
		FROM enterprises
		WHERE LOWER(BTRIM(portal_host)) = $1
	`, strings.ToLower(strings.TrimSpace(host))).Scan(&e.ID, &e.Name, &e.Host, &e.AdminUserID, &e.Status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errWrongHost
	}
	if err != nil {
		return nil, err
	}
	if e.Status != "active" {
		return nil, errEnterpriseInactive
	}
	return &e, nil
}

func maskEnterpriseEmail(email string) string {
	email = strings.TrimSpace(email)
	at := strings.LastIndexByte(email, '@')
	if at <= 0 || at == len(email)-1 {
		return "已配置"
	}
	local := email[:at]
	if len(local) == 1 {
		return "*" + email[at:]
	}
	return local[:1] + "***" + email[at:]
}

func (s *Service) CreateEnterprise(ctx context.Context, input CreateEnterpriseInput, actorUserID int64) (*PlatformEnterprise, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(input.Host)), ".")
	input.Reason = strings.TrimSpace(input.Reason)
	if input.Name == "" || len(input.Name) > 255 || input.Host == "" || strings.ContainsAny(input.Host, "/: ") || input.DedicatedUpstreamUser <= 0 || input.Reason == "" {
		return nil, infraerrors.BadRequest("INVALID_ENTERPRISE", "enterprise name, host, upstream user and reason are required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var upstreamEmail, upstreamStatus string
	if err = tx.QueryRowContext(ctx, `
		SELECT email, status FROM users WHERE id = $1 FOR SHARE
	`, input.DedicatedUpstreamUser).Scan(&upstreamEmail, &upstreamStatus); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errDedicatedUserUnavailable
		}
		return nil, err
	}
	if upstreamStatus != "active" {
		return nil, errDedicatedUserUnavailable
	}
	var subscriptionAvailable bool
	if err = tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM user_subscriptions AS subscription
			JOIN groups AS group_record ON group_record.id = subscription.group_id
			WHERE subscription.user_id = $1
			  AND subscription.status = 'active'
			  AND subscription.starts_at <= NOW()
			  AND subscription.expires_at > NOW()
			  AND group_record.status = 'active'
			  AND COALESCE(group_record.weekly_limit_usd, 0) > 0
		)
	`, input.DedicatedUpstreamUser).Scan(&subscriptionAvailable); err != nil {
		return nil, err
	}
	if !subscriptionAvailable {
		return nil, errSubscriptionUnavailable
	}

	result := new(PlatformEnterprise)
	err = tx.QueryRowContext(ctx, `
		INSERT INTO enterprises (name, dedicated_upstream_user_id, admin_user_id, portal_host, status)
		VALUES ($1, $2, $2, $3, 'active')
		RETURNING id, name, LOWER(BTRIM(portal_host)), dedicated_upstream_user_id, status, created_at`,
		input.Name, input.DedicatedUpstreamUser, input.Host).Scan(&result.ID, &result.Name, &result.Host, &result.DedicatedUpstreamUser, &result.Status, &result.CreatedAt)
	if err != nil {
		return nil, errConflict
	}
	result.AdminEmail = maskEnterpriseEmail(upstreamEmail)
	actor := fmt.Sprintf("platform_user:%d", actorUserID)
	payload, marshalErr := json.Marshal(map[string]any{"result": "success", "reason": input.Reason})
	if marshalErr != nil {
		return nil, marshalErr
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO enterprise_audit_events (enterprise_id, event_type, entity_type, entity_id, payload, actor_ref) VALUES ($1, 'enterprise.created', 'enterprise', $1, $2::jsonb, $3)`, result.ID, payload, actor); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Service) ListPlatformEnterprises(ctx context.Context, search, status string) ([]PlatformEnterprise, error) {
	conditions := []string{"1 = 1"}
	args := make([]any, 0, 2)
	if search = strings.TrimSpace(search); search != "" {
		args = append(args, "%"+strings.ToLower(search)+"%")
		conditions = append(conditions, "(LOWER(name) LIKE $1 OR LOWER(portal_host) LIKE $1)")
	}
	if status = strings.TrimSpace(status); status != "" {
		args = append(args, status)
		conditions = append(conditions, fmt.Sprintf("status = $%d", len(args)))
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.id, e.name, LOWER(BTRIM(e.portal_host)), e.dedicated_upstream_user_id, e.status, e.created_at,
		       COALESCE((SELECT email FROM users WHERE id = e.admin_user_id), ''),
		       (SELECT COUNT(*) FROM enterprise_employees WHERE enterprise_id = e.id),
		       (SELECT COUNT(*) FROM enterprise_employees WHERE enterprise_id = e.id AND status = 'active'),
		       (SELECT COUNT(*) FROM enterprise_sessions WHERE enterprise_id = e.id AND revoked_at IS NULL AND expires_at > NOW()),
		       (SELECT COUNT(*) FROM enterprise_key_assignments AS assignment
		          JOIN api_keys AS api_key ON api_key.id = assignment.api_key_id
		          WHERE assignment.enterprise_id = e.id AND assignment.status = 'active' AND api_key.status = 'active')
		FROM enterprises AS e WHERE `+strings.Join(conditions, " AND ")+` ORDER BY e.id DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]PlatformEnterprise, 0)
	for rows.Next() {
		var item PlatformEnterprise
		var adminEmail string
		if err := rows.Scan(&item.ID, &item.Name, &item.Host, &item.DedicatedUpstreamUser, &item.Status, &item.CreatedAt,
			&adminEmail, &item.EmployeeCount, &item.ActiveEmployeeCount, &item.ActiveSessionCount, &item.ActiveKeyCount); err != nil {
			return nil, err
		}
		item.AdminEmail = maskEnterpriseEmail(adminEmail)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) GetPlatformEnterprise(ctx context.Context, id int64) (*PlatformEnterprise, error) {
	var item PlatformEnterprise
	err := s.db.QueryRowContext(ctx, `SELECT id, name, LOWER(BTRIM(portal_host)), dedicated_upstream_user_id, status, created_at FROM enterprises WHERE id = $1`, id).Scan(&item.ID, &item.Name, &item.Host, &item.DedicatedUpstreamUser, &item.Status, &item.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errNotFound
	}
	if err != nil {
		return nil, err
	}
	var subscriptionsJSON []byte
	err = s.db.QueryRowContext(ctx, `
		SELECT COALESCE((SELECT email FROM users WHERE id = e.admin_user_id), ''),
		       COALESCE((SELECT jsonb_agg(jsonb_build_object(
					'id', es.id, 'status', es.status, 'plan', g.name,
					'weekly_limit', COALESCE(g.weekly_limit_usd::text, ''),
					'weekly_window_start', es.observed_weekly_window_start,
					'starts_at', us.starts_at, 'expires_at', us.expires_at
					) ORDER BY CASE WHEN es.status = 'active' THEN 0 ELSE 1 END, us.starts_at, es.id
					) FROM enterprise_subscriptions AS es
					JOIN user_subscriptions AS us ON us.id = es.upstream_user_subscription_id
					JOIN groups AS g ON g.id = us.group_id
					WHERE es.enterprise_id = e.id AND es.status IN ('active', 'scheduled')
				), '[]'::jsonb)
		FROM enterprises AS e WHERE e.id = $1`, id).Scan(&item.AdminEmail, &subscriptionsJSON)
	if err != nil {
		return nil, err
	}
	item.AdminEmail = maskEnterpriseEmail(item.AdminEmail)
	if len(subscriptionsJSON) > 0 {
		if err = json.Unmarshal(subscriptionsJSON, &item.Subscriptions); err != nil {
			return nil, err
		}
	}
	err = s.db.QueryRowContext(ctx, `
		SELECT
			(SELECT COUNT(*) FROM enterprise_employees WHERE enterprise_id = $1),
			(SELECT COUNT(*) FROM enterprise_employees WHERE enterprise_id = $1 AND status = 'active'),
			(SELECT COUNT(*) FROM enterprise_sessions WHERE enterprise_id = $1 AND revoked_at IS NULL AND expires_at > NOW()),
			(SELECT COUNT(*) FROM enterprise_key_assignments AS assignment
			 JOIN api_keys AS api_key ON api_key.id = assignment.api_key_id
			 WHERE assignment.enterprise_id = $1 AND assignment.status = 'active' AND api_key.status = 'active')`, id).
		Scan(&item.EmployeeCount, &item.ActiveEmployeeCount, &item.ActiveSessionCount, &item.ActiveKeyCount)
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (s *Service) DisableEnterprise(ctx context.Context, id, actorUserID int64, reason string) error {
	reason = strings.TrimSpace(reason)
	if id <= 0 || reason == "" {
		return infraerrors.BadRequest("INVALID_ENTERPRISE", "enterprise id and reason are required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE enterprises SET status = 'disabled', updated_at = NOW() WHERE id = $1 AND status = 'active'`, id)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return errNotFound
	}
	if _, err = tx.ExecContext(ctx, `UPDATE enterprise_sessions SET revoked_at = COALESCE(revoked_at, NOW()), updated_at = NOW() WHERE enterprise_id = $1 AND revoked_at IS NULL`, id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `
		UPDATE enterprise_key_assignments
		SET status = 'revoked',
		    ended_at = COALESCE(ended_at, NOW()),
		    revoked_at = COALESCE(revoked_at, NOW()),
		    updated_at = NOW()
		WHERE enterprise_id = $1 AND status = 'active'
	`, id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `
		UPDATE api_keys
		SET status = 'disabled', updated_at = NOW()
		WHERE status = 'active' AND id IN (
			SELECT api_key_id FROM enterprise_key_assignments WHERE enterprise_id = $1
		)
	`, id); err != nil {
		return err
	}
	payload, err := json.Marshal(map[string]any{"result": "success", "reason": reason, "keys_disabled": true})
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO enterprise_audit_events (enterprise_id, event_type, entity_type, entity_id, payload, actor_ref) VALUES ($1, 'enterprise.disabled', 'enterprise', $1, $2::jsonb, $3)`, id, payload, fmt.Sprintf("platform_user:%d", actorUserID)); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) Login(ctx context.Context, host, email, password, userAgent, ip string) (*TokenPair, error) {
	e, err := s.enterpriseByHost(ctx, host)
	if err != nil {
		return nil, err
	}
	email = strings.ToLower(strings.TrimSpace(email))
	var p Principal
	var passwordHash, status string
	var expires sql.NullTime
	err = s.db.QueryRowContext(ctx, `
		SELECT 'admin', users.id, users.email, 'enterprise_admin', users.password_hash,
		       users.status, 0::bigint, FALSE, NULL::timestamptz
		FROM users
		WHERE users.id = $1 AND LOWER(BTRIM(users.email)) = $2
		UNION ALL
		SELECT 'employee', employee.id, employee.current_email, 'enterprise_employee', employee.password_hash,
		       employee.status, employee.auth_version, employee.must_change_password,
		       employee.initial_password_expires_at
		FROM enterprise_employees AS employee
		WHERE employee.enterprise_id = $3
		  AND employee.status <> 'terminated'
		  AND LOWER(BTRIM(employee.current_email)) = $2
		LIMIT 1
	`, e.AdminUserID, email, e.ID).Scan(
		&p.PrincipalType, &p.PrincipalID, &p.Email, &p.Role, &passwordHash,
		&status, &p.AuthVersion, &p.ForceChange, &expires,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errInvalidCredentials
	}
	if err != nil {
		return nil, err
	}
	if bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(password)) != nil {
		return nil, errInvalidCredentials
	}
	if status != "active" {
		return nil, errPrincipalInactive
	}
	if p.PrincipalType == "employee" && p.ForceChange && expires.Valid && !s.now().Before(expires.Time) {
		return nil, errPasswordExpired
	}
	p.EnterpriseID = e.ID
	if p.PrincipalType == "admin" {
		p.AuthVersion = platformservice.ResolveTokenVersion(p.Email, passwordHash, 0)
	}
	return s.createSession(ctx, p, userAgent, ip)
}

func (s *Service) createSession(ctx context.Context, p Principal, userAgent, ip string) (*TokenPair, error) {
	sessionID := uuid.New()
	familyID := uuid.New()
	refresh, err := randomToken()
	if err != nil {
		return nil, err
	}
	expires := s.now().Add(refreshTokenTTL)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO enterprise_sessions (
			id, enterprise_id, principal_type, principal_id, refresh_family_id,
			auth_version, user_agent, ip_address, expires_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, sessionID, p.EnterpriseID, p.PrincipalType, p.PrincipalID, familyID,
		p.AuthVersion, truncate(userAgent, 512), truncate(ip, 64), expires)
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO enterprise_refresh_tokens (id, enterprise_id, session_id, refresh_family_id, token_hash, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, uuid.New(), p.EnterpriseID, sessionID, familyID, tokenHash(refresh), expires)
	if err != nil {
		return nil, err
	}
	pair, err := s.tokenPair(p, sessionID.String(), refresh)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return pair, nil
}

func (s *Service) tokenPair(p Principal, sessionID, refresh string) (*TokenPair, error) {
	now := s.now()
	claims := Claims{
		EnterpriseID: p.EnterpriseID, PrincipalType: p.PrincipalType, PrincipalID: p.PrincipalID,
		AuthVersion: p.AuthVersion, Role: p.Role, SessionID: sessionID, ForceChange: p.ForceChange,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:  p.PrincipalType + ":" + sessionID,
			IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(accessTokenTTL)),
		},
	}
	access, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.secret)
	if err != nil {
		return nil, err
	}
	return &TokenPair{AccessToken: access, RefreshToken: refresh, TokenType: "Bearer", ExpiresIn: int(accessTokenTTL.Seconds()), Principal: p}, nil
}

func (s *Service) Authenticate(ctx context.Context, host, raw string) (*Claims, *Principal, error) {
	if len(s.secret) == 0 {
		return nil, nil, errInvalidToken
	}
	claims := new(Claims)
	token, err := jwt.ParseWithClaims(raw, claims, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, errInvalidToken
		}
		return s.secret, nil
	})
	if err != nil || !token.Valid {
		return nil, nil, errInvalidToken
	}
	e, err := s.enterpriseByHost(ctx, host)
	if err != nil {
		return nil, nil, err
	}
	if e.ID != claims.EnterpriseID {
		return nil, nil, errWrongHost
	}
	p, err := s.currentPrincipal(ctx, e.ID, claims.PrincipalType, claims.PrincipalID)
	if err != nil {
		return nil, nil, err
	}
	if p.AuthVersion != claims.AuthVersion {
		return nil, nil, errInvalidToken
	}
	var active bool
	err = s.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM enterprise_sessions
			WHERE enterprise_id = $1 AND id = $2 AND revoked_at IS NULL AND expires_at > NOW()
		)
	`, e.ID, claims.SessionID).Scan(&active)
	if err != nil {
		return nil, nil, err
	}
	if !active {
		return nil, nil, errInvalidToken
	}
	return claims, p, nil
}

func (s *Service) currentPrincipal(ctx context.Context, enterpriseID int64, principalType string, principalID int64) (*Principal, error) {
	return s.currentPrincipalFrom(ctx, s.db, enterpriseID, principalType, principalID)
}

func (s *Service) currentPrincipalFrom(ctx context.Context, db queryRower, enterpriseID int64, principalType string, principalID int64) (*Principal, error) {
	var p Principal
	p.EnterpriseID, p.PrincipalType, p.PrincipalID = enterpriseID, principalType, principalID
	var status string
	switch principalType {
	case "admin":
		var passwordHash string
		err := db.QueryRowContext(ctx, `
			SELECT users.email, users.password_hash, users.status
			FROM enterprises JOIN users ON users.id = enterprises.admin_user_id
			WHERE enterprises.id = $1 AND users.id = $2
		`, enterpriseID, principalID).Scan(&p.Email, &passwordHash, &status)
		if err != nil {
			return nil, errInactive
		}
		p.AuthVersion = platformservice.ResolveTokenVersion(p.Email, passwordHash, 0)
		p.Role = "enterprise_admin"
	case "employee":
		err := db.QueryRowContext(ctx, `
			SELECT current_email, status, auth_version, must_change_password
			FROM enterprise_employees
			WHERE enterprise_id = $1 AND id = $2
		`, enterpriseID, principalID).Scan(&p.Email, &status, &p.AuthVersion, &p.ForceChange)
		if err != nil {
			return nil, errInactive
		}
		p.Role = "enterprise_employee"
	default:
		return nil, errInvalidToken
	}
	if status != "active" {
		return nil, errPrincipalInactive
	}
	return &p, nil
}

func (s *Service) Refresh(ctx context.Context, host, refresh, userAgent, ip string) (*TokenPair, error) {
	e, err := s.enterpriseByHost(ctx, host)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var sessionID, principalType, familyID string
	var principalID, authVersion int64
	var tokenExpires, sessionExpires time.Time
	var consumedAt, tokenRevokedAt, sessionRevokedAt sql.NullTime
	err = tx.QueryRowContext(ctx, `
		SELECT session.id, session.principal_type, session.principal_id, session.refresh_family_id,
		       session.auth_version, token.expires_at, token.consumed_at, token.revoked_at,
		       session.expires_at, session.revoked_at
		FROM enterprise_refresh_tokens AS token
		JOIN enterprise_sessions AS session
		  ON session.enterprise_id = token.enterprise_id AND session.id = token.session_id
		WHERE token.enterprise_id = $1 AND token.token_hash = $2
		FOR UPDATE OF token, session
	`, e.ID, tokenHash(refresh)).Scan(&sessionID, &principalType, &principalID, &familyID,
		&authVersion, &tokenExpires, &consumedAt, &tokenRevokedAt, &sessionExpires, &sessionRevokedAt)
	if err != nil {
		return nil, errInvalidToken
	}
	if consumedAt.Valid {
		if err := revokeRefreshFamily(ctx, tx, e.ID, familyID); err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return nil, errInvalidToken
	}
	if tokenRevokedAt.Valid || sessionRevokedAt.Valid || !s.now().Before(tokenExpires) || !s.now().Before(sessionExpires) {
		return nil, errInvalidToken
	}
	p, err := s.currentPrincipalFrom(ctx, tx, e.ID, principalType, principalID)
	if err != nil || p.AuthVersion != authVersion {
		_ = revokeRefreshFamily(ctx, tx, e.ID, familyID)
		return nil, errInvalidToken
	}
	nextRefresh, err := randomToken()
	if err != nil {
		return nil, err
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE enterprise_refresh_tokens SET consumed_at = NOW()
		WHERE enterprise_id = $1 AND token_hash = $2 AND consumed_at IS NULL AND revoked_at IS NULL
	`, e.ID, tokenHash(refresh))
	if err != nil {
		return nil, err
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return nil, errInvalidToken
	}
	if _, err = tx.ExecContext(ctx, `
		UPDATE enterprise_sessions SET user_agent = $1, ip_address = $2, last_seen_at = NOW(), updated_at = NOW()
		WHERE enterprise_id = $3 AND id = $4 AND revoked_at IS NULL
	`, truncate(userAgent, 512), truncate(ip, 64), e.ID, sessionID); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO enterprise_refresh_tokens (id, enterprise_id, session_id, refresh_family_id, token_hash, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, uuid.New(), e.ID, sessionID, familyID, tokenHash(nextRefresh), tokenExpires); err != nil {
		return nil, err
	}
	pair, err := s.tokenPair(*p, sessionID, nextRefresh)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return pair, nil
}

func (s *Service) Logout(ctx context.Context, host, refresh string) error {
	e, err := s.enterpriseByHost(ctx, host)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var familyID string
	err = tx.QueryRowContext(ctx, `
		SELECT token.refresh_family_id
		FROM enterprise_refresh_tokens AS token
		JOIN enterprise_sessions AS session
		  ON session.enterprise_id = token.enterprise_id AND session.id = token.session_id
		WHERE token.enterprise_id = $1 AND token.token_hash = $2
		  AND token.consumed_at IS NULL AND token.revoked_at IS NULL AND token.expires_at > NOW()
		  AND session.revoked_at IS NULL AND session.expires_at > NOW()
		FOR UPDATE OF token, session
	`, e.ID, tokenHash(refresh)).Scan(&familyID)
	if err != nil {
		return errInvalidToken
	}
	if err := revokeRefreshFamily(ctx, tx, e.ID, familyID); err != nil {
		return err
	}
	return tx.Commit()
}

func revokeRefreshFamily(ctx context.Context, tx *sql.Tx, enterpriseID int64, familyID string) error {
	if _, err := tx.ExecContext(ctx, `
		UPDATE enterprise_sessions SET revoked_at = COALESCE(revoked_at, NOW()), updated_at = NOW()
		WHERE enterprise_id = $1 AND refresh_family_id = $2
	`, enterpriseID, familyID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
		UPDATE enterprise_refresh_tokens SET revoked_at = COALESCE(revoked_at, NOW())
		WHERE enterprise_id = $1 AND refresh_family_id = $2
	`, enterpriseID, familyID)
	return err
}

func (s *Service) ChangeInitialPassword(ctx context.Context, claims *Claims, current, next string) error {
	if claims.PrincipalType != "employee" {
		return infraerrors.BadRequest("EMPLOYEE_PASSWORD_ONLY", "enterprise administrator uses the platform password")
	}
	if len(next) < 12 {
		return infraerrors.BadRequest("WEAK_PASSWORD", "password must be at least 12 characters")
	}
	var hash string
	err := s.db.QueryRowContext(ctx, `
		SELECT password_hash FROM enterprise_employees
		WHERE enterprise_id = $1 AND id = $2 AND status = 'active'
	`, claims.EnterpriseID, claims.PrincipalID).Scan(&hash)
	if err != nil || bcrypt.CompareHashAndPassword([]byte(hash), []byte(current)) != nil {
		return errInvalidCredentials
	}
	nextHash, err := bcrypt.GenerateFromPassword([]byte(next), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `
		UPDATE enterprise_employees
		SET password_hash = $1, must_change_password = FALSE, initial_password_expires_at = NULL,
		    password_changed_at = NOW(), auth_version = auth_version + 1, updated_at = NOW()
		WHERE enterprise_id = $2 AND id = $3 AND status = 'active'
	`, string(nextHash), claims.EnterpriseID, claims.PrincipalID)
	if err == nil {
		_, err = tx.ExecContext(ctx, `UPDATE enterprise_sessions SET revoked_at = NOW(), updated_at = NOW()
			WHERE enterprise_id = $1 AND principal_type = 'employee' AND principal_id = $2 AND revoked_at IS NULL`, claims.EnterpriseID, claims.PrincipalID)
	}
	if err != nil {
		return err
	}
	if err := writeAuditEvent(ctx, tx, claims.EnterpriseID, "employee.password_changed", "employee", &claims.PrincipalID, map[string]any{"result": "success", "initial": true}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) ChangePassword(ctx context.Context, claims *Claims, current, next string) error {
	if claims == nil || claims.PrincipalType != "employee" {
		return infraerrors.BadRequest("EMPLOYEE_PASSWORD_ONLY", "enterprise administrator uses the platform password")
	}
	if len(next) < 12 {
		return infraerrors.BadRequest("WEAK_PASSWORD", "password must be at least 12 characters")
	}
	var currentHash string
	if err := s.db.QueryRowContext(ctx, `SELECT password_hash FROM enterprise_employees WHERE enterprise_id = $1 AND id = $2 AND status = 'active'`, claims.EnterpriseID, claims.PrincipalID).Scan(&currentHash); err != nil || bcrypt.CompareHashAndPassword([]byte(currentHash), []byte(current)) != nil {
		return errInvalidCredentials
	}
	nextHash, err := bcrypt.GenerateFromPassword([]byte(next), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE enterprise_employees SET password_hash = $1, must_change_password = FALSE, initial_password_expires_at = NULL, password_changed_at = NOW(), auth_version = auth_version + 1, updated_at = NOW() WHERE enterprise_id = $2 AND id = $3 AND status = 'active'`, string(nextHash), claims.EnterpriseID, claims.PrincipalID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE enterprise_sessions SET revoked_at = NOW(), updated_at = NOW() WHERE enterprise_id = $1 AND principal_type = 'employee' AND principal_id = $2 AND revoked_at IS NULL`, claims.EnterpriseID, claims.PrincipalID); err != nil {
		return err
	}
	if err = writeAuditEvent(ctx, tx, claims.EnterpriseID, "employee.password_changed", "employee", &claims.PrincipalID, map[string]any{"result": "success"}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) RequestReset(ctx context.Context, host, email, resetBaseURL, locale string) error {
	e, err := s.enterpriseByHost(ctx, host)
	if err != nil {
		return nil
	}
	var employeeID int64
	var currentEmail string
	err = s.db.QueryRowContext(ctx, `
		SELECT id, current_email FROM enterprise_employees
		WHERE enterprise_id = $1 AND status = 'active' AND LOWER(BTRIM(current_email)) = LOWER(BTRIM($2))
	`, e.ID, email).Scan(&employeeID, &currentEmail)
	if errors.Is(err, sql.ErrNoRows) {
		return s.recordAuditEvent(ctx, e.ID, "password.reset_requested", "employee", nil, map[string]any{"result": "success", "reason": "request_accepted"})
	}
	if err != nil {
		return err
	}
	raw, err := randomToken()
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO enterprise_password_reset_tokens (id, enterprise_id, employee_id, token_hash, expires_at)
		VALUES ($1, $2, $3, $4, $5)
	`, uuid.New(), e.ID, employeeID, tokenHash(raw), s.now().Add(passwordResetTTL))
	if err != nil {
		return err
	}
	delivered := false
	if s.mailer != nil {
		resetURL := strings.TrimRight(resetBaseURL, "/") + "?token=" + url.QueryEscape(raw)
		subject := fmt.Sprintf("[%s] Password reset", e.Name)
		body := fmt.Sprintf("<p>Use this link within one hour to reset your password:</p><p><a href=\"%s\">%s</a></p>", html.EscapeString(resetURL), html.EscapeString(resetURL))
		delivered = s.mailer.SendEmail(ctx, currentEmail, subject, body) == nil
	}
	if !delivered {
		_, _ = s.db.ExecContext(ctx, `
			DELETE FROM enterprise_password_reset_tokens
			WHERE enterprise_id = $1 AND employee_id = $2 AND token_hash = $3 AND used_at IS NULL
		`, e.ID, employeeID, tokenHash(raw))
	}
	return s.recordAuditEvent(ctx, e.ID, "password.reset_requested", "employee", &employeeID, map[string]any{"result": "success", "reason": "request_accepted"})
}

func (s *Service) ResetPassword(ctx context.Context, host, token, next string) error {
	e, err := s.enterpriseByHost(ctx, host)
	if err != nil {
		return errResetInvalid
	}
	if len(next) < 12 {
		_ = s.RecordRejectedAuditEvent(ctx, e.ID, "password.reset", "employee", nil, "weak_password")
		return infraerrors.BadRequest("WEAK_PASSWORD", "password must be at least 12 characters")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(next), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var tokenID string
	var employeeID int64
	err = tx.QueryRowContext(ctx, `
		SELECT reset.id, reset.employee_id
		FROM enterprise_password_reset_tokens AS reset
		JOIN enterprise_employees AS employee
		  ON employee.enterprise_id = reset.enterprise_id AND employee.id = reset.employee_id
		WHERE reset.enterprise_id = $1 AND reset.token_hash = $2
		  AND reset.used_at IS NULL AND reset.expires_at > NOW() AND employee.status = 'active'
		FOR UPDATE OF reset, employee
	`, e.ID, tokenHash(token)).Scan(&tokenID, &employeeID)
	if err != nil {
		_ = tx.Rollback()
		_ = s.RecordRejectedAuditEvent(ctx, e.ID, "password.reset", "employee", nil, "invalid_token")
		return errResetInvalid
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE enterprise_password_reset_tokens SET used_at = NOW()
		WHERE enterprise_id = $1 AND id = $2 AND used_at IS NULL
	`, e.ID, tokenID)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n != 1 {
		_ = tx.Rollback()
		_ = s.RecordRejectedAuditEvent(ctx, e.ID, "password.reset", "employee", &employeeID, "invalid_token")
		return errResetInvalid
	}
	_, err = tx.ExecContext(ctx, `
		UPDATE enterprise_employees
		SET password_hash = $1, must_change_password = FALSE, initial_password_expires_at = NULL,
		    password_changed_at = NOW(), auth_version = auth_version + 1, updated_at = NOW()
		WHERE enterprise_id = $2 AND id = $3 AND status = 'active'
	`, string(hash), e.ID, employeeID)
	if err == nil {
		_, err = tx.ExecContext(ctx, `UPDATE enterprise_sessions SET revoked_at = NOW(), updated_at = NOW()
			WHERE enterprise_id = $1 AND principal_type = 'employee' AND principal_id = $2 AND revoked_at IS NULL`, e.ID, employeeID)
	}
	if err != nil {
		return err
	}
	if err = writeAuditEvent(ctx, tx, e.ID, "password.reset", "employee", &employeeID, map[string]any{"result": "success"}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) ListSessions(ctx context.Context, claims *Claims) ([]Session, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, user_agent, ip_address, last_seen_at, created_at, expires_at, revoked_at
		FROM enterprise_sessions
		WHERE enterprise_id = $1 AND principal_type = $2 AND principal_id = $3
		ORDER BY created_at DESC
	`, claims.EnterpriseID, claims.PrincipalType, claims.PrincipalID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Session, 0)
	for rows.Next() {
		var item Session
		var revoked sql.NullTime
		if err := rows.Scan(&item.ID, &item.UserAgent, &item.IPAddress, &item.LastSeenAt, &item.CreatedAt, &item.ExpiresAt, &revoked); err != nil {
			return nil, err
		}
		if revoked.Valid {
			item.RevokedAt = &revoked.Time
		}
		item.Current = item.ID == claims.SessionID
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) RevokeSession(ctx context.Context, claims *Claims, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `
		UPDATE enterprise_sessions SET revoked_at = COALESCE(revoked_at, NOW()), updated_at = NOW()
		WHERE enterprise_id = $1 AND principal_type = $2 AND principal_id = $3 AND id = $4
	`, claims.EnterpriseID, claims.PrincipalType, claims.PrincipalID, id)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		_ = tx.Rollback()
		_ = s.RecordRejectedAuditEvent(ctx, claims.EnterpriseID, "session.revoke", "session", nil, "not_found")
		return errNotFound
	}
	if err = writeAuditEvent(ctx, tx, claims.EnterpriseID, "session.revoke", "session", nil, map[string]any{"result": "success"}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) RevokeAllSessions(ctx context.Context, claims *Claims) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `
		UPDATE enterprise_sessions SET revoked_at = COALESCE(revoked_at, NOW()), updated_at = NOW()
		WHERE enterprise_id = $1 AND principal_type = $2 AND principal_id = $3 AND revoked_at IS NULL
	`, claims.EnterpriseID, claims.PrincipalType, claims.PrincipalID); err != nil {
		return err
	}
	if err = writeAuditEvent(ctx, tx, claims.EnterpriseID, "session.revoke_all", "session", nil, map[string]any{"result": "success"}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) ListDepartments(ctx context.Context, enterpriseID int64) ([]Department, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, created_at FROM enterprise_departments WHERE enterprise_id = $1 AND status = 'active' ORDER BY name`, enterpriseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Department, 0)
	for rows.Next() {
		var item Department
		if err := rows.Scan(&item.ID, &item.Name, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) CreateDepartment(ctx context.Context, enterpriseID int64, name string) (*Department, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 100 {
		return nil, infraerrors.BadRequest("INVALID_DEPARTMENT", "department name is invalid")
	}
	item := new(Department)
	actor := auditActor(ctx)
	if actor == "" {
		err := s.db.QueryRowContext(ctx, `INSERT INTO enterprise_departments (enterprise_id, name) VALUES ($1, $2) RETURNING id, name, created_at`, enterpriseID, name).Scan(&item.ID, &item.Name, &item.CreatedAt)
		if err != nil {
			return nil, errConflict
		}
		return item, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	err = tx.QueryRowContext(ctx, `INSERT INTO enterprise_departments (enterprise_id, name) VALUES ($1, $2) RETURNING id, name, created_at`, enterpriseID, name).Scan(&item.ID, &item.Name, &item.CreatedAt)
	if err != nil {
		return nil, errConflict
	}
	if err := writeAuditEvent(ctx, tx, enterpriseID, "department.created", "department", &item.ID, map[string]any{"result": "success", "name": item.Name}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return item, nil
}

// PreviewDepartmentDeletion reports how many active employees would be
// detached before an admin confirms an irreversible department deletion.
func (s *Service) PreviewDepartmentDeletion(ctx context.Context, enterpriseID, departmentID int64) (*DepartmentDeletionImpact, error) {
	impact := &DepartmentDeletionImpact{DepartmentID: departmentID}
	err := s.db.QueryRowContext(ctx, `
		SELECT department.name,
		       (SELECT COUNT(*) FROM enterprise_employees
		        WHERE enterprise_id = $1 AND department_id = $2 AND status <> 'terminated')
		FROM enterprise_departments AS department
		WHERE department.enterprise_id = $1 AND department.id = $2 AND department.status = 'active'
	`, enterpriseID, departmentID).Scan(&impact.DepartmentName, &impact.AffectedEmployees)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errNotFound
	}
	if err != nil {
		return nil, err
	}
	return impact, nil
}

func (s *Service) DeleteDepartment(ctx context.Context, enterpriseID, departmentID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var departmentName string
	if err := tx.QueryRowContext(ctx, `SELECT name FROM enterprise_departments WHERE enterprise_id = $1 AND id = $2 AND status = 'active' FOR UPDATE`, enterpriseID, departmentID).Scan(&departmentName); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errNotFound
		}
		return err
	}
	clearResult, err := tx.ExecContext(ctx, `UPDATE enterprise_employees SET department_id = NULL, updated_at = NOW() WHERE enterprise_id = $1 AND department_id = $2`, enterpriseID, departmentID)
	if err != nil {
		return err
	}
	affected, _ := clearResult.RowsAffected()
	result, err := tx.ExecContext(ctx, `UPDATE enterprise_departments SET status = 'disabled', disabled_at = NOW(), updated_at = NOW() WHERE enterprise_id = $1 AND id = $2 AND status = 'active'`, enterpriseID, departmentID)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return errNotFound
	}
	if err := writeAuditEvent(ctx, tx, enterpriseID, "department.deleted", "department", &departmentID, map[string]any{"result": "success", "name": departmentName, "affected_employees": affected}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) ListEmployees(ctx context.Context, enterpriseID int64) ([]Employee, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, COALESCE(current_email, email), status, department_id, must_change_password, terminated_at, version FROM enterprise_employees WHERE enterprise_id = $1 ORDER BY id`, enterpriseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Employee, 0)
	for rows.Next() {
		var item Employee
		var dept sql.NullInt64
		var terminated sql.NullTime
		if err := rows.Scan(&item.ID, &item.Email, &item.Status, &dept, &item.MustChange, &terminated, &item.Version); err != nil {
			return nil, err
		}
		if dept.Valid {
			item.DepartmentID = &dept.Int64
		}
		if terminated.Valid {
			item.TerminatedAt = &terminated.Time
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// GetEmployee loads a single employee scoped to the caller's enterprise. A
// cross-enterprise or unknown id returns the same errNotFound so the
// response never reveals whether the id exists in another tenant.
func (s *Service) GetEmployee(ctx context.Context, enterpriseID, employeeID int64) (*EmployeeDetail, error) {
	item := new(EmployeeDetail)
	var dept sql.NullInt64
	var deptName sql.NullString
	var terminated sql.NullTime
	err := s.db.QueryRowContext(ctx, `
		SELECT employee.id, COALESCE(employee.current_email, employee.email), employee.status,
		       employee.department_id, department.name, employee.must_change_password,
		       employee.terminated_at, employee.version, employee.created_at
		FROM enterprise_employees AS employee
		LEFT JOIN enterprise_departments AS department
		  ON department.enterprise_id = employee.enterprise_id AND department.id = employee.department_id
		WHERE employee.enterprise_id = $1 AND employee.id = $2
	`, enterpriseID, employeeID).Scan(
		&item.ID, &item.Email, &item.Status, &dept, &deptName, &item.MustChange, &terminated, &item.Version, &item.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errNotFound
	}
	if err != nil {
		return nil, err
	}
	if dept.Valid {
		item.DepartmentID = &dept.Int64
	}
	if deptName.Valid {
		item.DepartmentName = &deptName.String
	}
	if terminated.Valid {
		item.TerminatedAt = &terminated.Time
	}
	return item, nil
}

// EnterprisePoolStatus is the read-only enterprise-wide counterpart to
// EmployeeUsageSummary, so e-03 can show a personal-vs-enterprise
// comparison. It is computed independently of GetEmployeeUsage to keep the
// two read paths decoupled; both now follow the same corrected
// groups.weekly_limit_usd join used by the admin workbench summary
// (SHAN-267 fixed GetEmployeeUsage's copy on origin/main).
type EnterprisePoolStatus struct {
	SourceStatus  string     `json:"source_status"`
	PoolLimit     string     `json:"pool_limit"`
	PoolUsed      string     `json:"pool_used"`
	PoolRemaining string     `json:"pool_remaining"`
	PoolExhausted bool       `json:"pool_exhausted"`
	WindowAnchor  *time.Time `json:"window_anchor,omitempty"`
}

func (s *Service) GetEnterprisePoolStatus(ctx context.Context, enterpriseID int64) (*EnterprisePoolStatus, error) {
	result := &EnterprisePoolStatus{SourceStatus: "unavailable", PoolLimit: "0", PoolUsed: "0", PoolRemaining: "0"}
	var poolStatus sql.NullString
	var poolLimit, poolUsed, poolRemaining sql.NullString
	var poolExhausted sql.NullBool
	var poolAnchor sql.NullTime
	err := s.db.QueryRowContext(ctx, `
		SELECT CASE WHEN enterprise_subscription.id IS NULL OR upstream_subscription.id IS NULL
		            OR subscription_group.weekly_limit_usd IS NULL OR subscription_group.weekly_limit_usd <= 0
		       THEN 'unavailable' ELSE 'available' END,
		       COALESCE(CASE WHEN subscription_group.weekly_limit_usd > 0 THEN subscription_group.weekly_limit_usd END, 0)::NUMERIC(20,8)::text,
		       COALESCE(upstream_subscription.weekly_usage_usd, 0)::NUMERIC(20,8)::text,
		       CASE WHEN subscription_group.weekly_limit_usd IS NULL OR subscription_group.weekly_limit_usd <= 0 THEN '0'
		            ELSE GREATEST(subscription_group.weekly_limit_usd - COALESCE(upstream_subscription.weekly_usage_usd, 0), 0)::NUMERIC(20,8)::text END,
		       (subscription_group.weekly_limit_usd IS NOT NULL AND subscription_group.weekly_limit_usd > 0
		        AND COALESCE(upstream_subscription.weekly_usage_usd, 0) >= subscription_group.weekly_limit_usd),
		       upstream_subscription.weekly_window_start
		FROM enterprises AS enterprise
		LEFT JOIN enterprise_subscriptions AS enterprise_subscription
		  ON enterprise_subscription.enterprise_id = enterprise.id AND enterprise_subscription.status = 'active'
		LEFT JOIN user_subscriptions AS upstream_subscription
		  ON upstream_subscription.id = enterprise_subscription.upstream_user_subscription_id
		 AND upstream_subscription.user_id = enterprise.dedicated_upstream_user_id
		 AND upstream_subscription.deleted_at IS NULL
		LEFT JOIN groups AS subscription_group ON subscription_group.id = upstream_subscription.group_id
		WHERE enterprise.id = $1 AND enterprise.status = 'active'`, enterpriseID).Scan(
		&poolStatus, &poolLimit, &poolUsed, &poolRemaining, &poolExhausted, &poolAnchor)
	if errors.Is(err, sql.ErrNoRows) {
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	if poolStatus.Valid {
		result.SourceStatus = poolStatus.String
	}
	if poolLimit.Valid {
		result.PoolLimit = poolLimit.String
	}
	if poolUsed.Valid {
		result.PoolUsed = poolUsed.String
	}
	if poolRemaining.Valid {
		result.PoolRemaining = poolRemaining.String
	}
	result.PoolExhausted = poolExhausted.Valid && poolExhausted.Bool
	if poolAnchor.Valid {
		anchor := poolAnchor.Time.UTC()
		result.WindowAnchor = &anchor
	}
	return result, nil
}

func (s *Service) GetEmployeeUsage(ctx context.Context, enterpriseID, employeeID int64) (*EmployeeUsageSummary, error) {
	result := &EmployeeUsageSummary{
		WindowType: "week", SourceStatus: "unavailable", Allocation: "0", ActualCost: "0", Remaining: "0", Overage: "0",
	}
	var employeeExists bool
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM enterprise_employees WHERE enterprise_id = $1 AND id = $2)`, enterpriseID, employeeID).Scan(&employeeExists); err != nil {
		return nil, err
	}
	if !employeeExists {
		return nil, errNotFound
	}
	var subscriptionID sql.NullInt64
	var anchor sql.NullTime
	var limit sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT enterprise_subscription.id, upstream_subscription.weekly_window_start,
		       subscription_group.weekly_limit_usd::text
		FROM enterprises AS enterprise
		JOIN enterprise_employees AS employee ON employee.enterprise_id = enterprise.id AND employee.id = $2
		LEFT JOIN enterprise_subscriptions AS enterprise_subscription
		  ON enterprise_subscription.enterprise_id = enterprise.id AND enterprise_subscription.status = 'active'
		LEFT JOIN user_subscriptions AS upstream_subscription
		  ON upstream_subscription.id = enterprise_subscription.upstream_user_subscription_id
		 AND upstream_subscription.user_id = enterprise.dedicated_upstream_user_id
		 AND upstream_subscription.deleted_at IS NULL
		LEFT JOIN groups AS subscription_group ON subscription_group.id = upstream_subscription.group_id
		WHERE enterprise.id = $1 AND enterprise.status = 'active'`, enterpriseID, employeeID).
		Scan(&subscriptionID, &anchor, &limit)
	if err != nil {
		return nil, err
	}
	if !subscriptionID.Valid || !anchor.Valid {
		return result, nil
	}
	result.SubscriptionID = &subscriptionID.Int64
	windowAnchor := anchor.Time.UTC()
	result.WindowAnchor = &windowAnchor
	result.SourceStatus = "available"
	var configured, actual string
	err = s.db.QueryRowContext(ctx, `
		SELECT COALESCE((SELECT amount FROM enterprise_weekly_allocations
		                WHERE enterprise_id = $1 AND subscription_id = $2 AND employee_id = $3
		                  AND window_type = 'week' AND window_anchor = $4
		                ORDER BY version DESC LIMIT 1), 0)::NUMERIC(20,8)::text,
		       COALESCE(SUM(COALESCE(usage_log.actual_cost, 0)), 0)::NUMERIC(20,8)::text,
		       COUNT(attribution.id)
		FROM enterprise_usage_attributions AS attribution
		LEFT JOIN usage_logs AS usage_log ON usage_log.id = attribution.usage_log_id
		WHERE attribution.enterprise_id = $1 AND attribution.subscription_id = $2
		  AND attribution.employee_id = $3 AND attribution.window_type = 'week'
		  AND attribution.window_anchor = $4 AND attribution.classification = 'employee'`,
		enterpriseID, subscriptionID.Int64, employeeID, windowAnchor).Scan(&configured, &actual, &result.Requests)
	if err != nil {
		return nil, err
	}
	result.Allocation, result.ActualCost = configured, actual
	result.Remaining = decimalMaxDifference(configured, actual)
	result.Overage = decimalMaxDifference(actual, configured)
	return result, nil
}

// ListEmployeeUsage returns the caller's own call-level attribution rows,
// scoped by enterprise+employee (never accepts a client-supplied identity)
// and bounded by an optional request_at window and page/page_size.
func (s *Service) ListEmployeeUsage(ctx context.Context, enterpriseID, employeeID int64, q EmployeeUsageQuery) ([]EmployeeUsageRecord, int64, error) {
	conditions := []string{"attribution.enterprise_id = $1", "attribution.employee_id = $2",
		"attribution.window_type = 'week'", "attribution.classification = 'employee'"}
	args := []any{enterpriseID, employeeID}
	add := func(condition string, value any) {
		args = append(args, value)
		conditions = append(conditions, fmt.Sprintf(condition, len(args)))
	}
	if q.StartAt != nil {
		add("attribution.request_at >= $%d", *q.StartAt)
	}
	if q.EndAt != nil {
		add("attribution.request_at < $%d", *q.EndAt)
	}
	where := strings.Join(conditions, " AND ")

	var total int64
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM enterprise_usage_attributions AS attribution WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	page, pageSize := q.Page, q.PageSize
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	limitPos := len(args) + 1
	offsetPos := len(args) + 2
	listArgs := append(append([]any{}, args...), pageSize, (page-1)*pageSize)

	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT attribution.request_at, attribution.window_anchor,
		       CASE WHEN LENGTH(api_key.key) <= 10 THEN '********'
		            ELSE SUBSTRING(api_key.key FROM 1 FOR 6) || '...' || RIGHT(api_key.key, 4) END,
		       attribution.assignment_generation,
		       COALESCE(usage_log.actual_cost, 0)::NUMERIC(20,8)::text
		FROM enterprise_usage_attributions AS attribution
		LEFT JOIN usage_logs AS usage_log ON usage_log.id = attribution.usage_log_id
		LEFT JOIN api_keys AS api_key ON api_key.id = attribution.api_key_id
		WHERE %s
		ORDER BY attribution.request_at DESC, attribution.id DESC LIMIT $%d OFFSET $%d`, where, limitPos, offsetPos), listArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]EmployeeUsageRecord, 0)
	for rows.Next() {
		var item EmployeeUsageRecord
		if err := rows.Scan(&item.RequestAt, &item.WindowAnchor, &item.APIKeyMasked, &item.Generation, &item.ActualCost); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

// EmployeeUsageTrendPoint is one real daily aggregation point over the
// caller's own attribution rows — never a fabricated or interpolated value.
// R1's scheduled/backfilled history isn't implemented yet, so a window with
// no attribution rows yields no points rather than a synthesized zero series.
type EmployeeUsageTrendPoint struct {
	At         time.Time `json:"at"`
	Requests   int64     `json:"requests"`
	ActualCost string    `json:"actual_cost"`
}

func (s *Service) ListEmployeeUsageTrend(ctx context.Context, enterpriseID, employeeID int64, q EmployeeUsageQuery) ([]EmployeeUsageTrendPoint, error) {
	conditions := []string{"attribution.enterprise_id = $1", "attribution.employee_id = $2",
		"attribution.window_type = 'week'", "attribution.classification = 'employee'"}
	args := []any{enterpriseID, employeeID}
	add := func(condition string, value any) {
		args = append(args, value)
		conditions = append(conditions, fmt.Sprintf(condition, len(args)))
	}
	if q.StartAt != nil {
		add("attribution.request_at >= $%d", *q.StartAt)
	}
	if q.EndAt != nil {
		add("attribution.request_at < $%d", *q.EndAt)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT DATE_TRUNC('day', attribution.request_at),
		       COUNT(*), COALESCE(SUM(COALESCE(usage_log.actual_cost, 0)), 0)::text
		FROM enterprise_usage_attributions AS attribution
		LEFT JOIN usage_logs AS usage_log ON usage_log.id = attribution.usage_log_id
		WHERE `+strings.Join(conditions, " AND ")+`
		GROUP BY 1 ORDER BY 1`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]EmployeeUsageTrendPoint, 0)
	for rows.Next() {
		var item EmployeeUsageTrendPoint
		if err := rows.Scan(&item.At, &item.Requests, &item.ActualCost); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func decimalMaxDifference(left, right string) string {
	l, ok := new(big.Float).SetPrec(256).SetString(strings.TrimSpace(left))
	if !ok {
		return "0"
	}
	r, ok := new(big.Float).SetPrec(256).SetString(strings.TrimSpace(right))
	if !ok {
		return "0"
	}
	if l.Sub(l, r).Sign() <= 0 {
		return "0"
	}
	return l.Text('f', 8)
}

func (s *Service) CreateEmployee(ctx context.Context, enterpriseID int64, email, password string, departmentID *int64) (*Employee, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" || len(password) < 12 {
		return nil, infraerrors.BadRequest("INVALID_EMPLOYEE", "employee email or password is invalid")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	item := new(Employee)
	var dept sql.NullInt64
	actor := auditActor(ctx)
	insertEmployee := func(q queryRower) error {
		return q.QueryRowContext(ctx, `
			INSERT INTO enterprise_employees (enterprise_id, email, current_email, password_hash, department_id, status, must_change_password, initial_password_expires_at)
		SELECT $1, $2, $2, $3, $4, 'active', TRUE, NOW() + INTERVAL '24 hours'
		WHERE $4::bigint IS NULL OR EXISTS (SELECT 1 FROM enterprise_departments WHERE enterprise_id = $1 AND id = $4 AND status = 'active')
		RETURNING id, current_email, status, department_id, must_change_password, version
		`, enterpriseID, email, string(hash), departmentID).Scan(&item.ID, &item.Email, &item.Status, &dept, &item.MustChange, &item.Version)
	}
	if actor == "" {
		err = insertEmployee(s.db)
	} else {
		tx, txErr := s.db.BeginTx(ctx, nil)
		if txErr != nil {
			return nil, txErr
		}
		defer tx.Rollback()
		err = insertEmployee(tx)
		if err == nil {
			if auditErr := writeAuditEvent(ctx, tx, enterpriseID, "employee.created", "employee", &item.ID, map[string]any{"result": "success", "employee_id": item.ID, "department_id": departmentID}); auditErr != nil {
				return nil, auditErr
			}
		}
		if err == nil {
			err = tx.Commit()
		}
	}
	if err != nil {
		return nil, errConflict
	}
	if dept.Valid {
		item.DepartmentID = &dept.Int64
	}
	return item, nil
}

// UpdateEmployee applies an admin edit using optimistic concurrency: the
// caller must supply the version it last observed. A mismatch means another
// admin edited the same employee concurrently, so the write is rejected with
// zero partial effect instead of silently overwriting the other admin's change.
func (s *Service) UpdateEmployee(ctx context.Context, enterpriseID, employeeID int64, status string, departmentID *int64, expectedVersion int64) error {
	if status != "active" && status != "disabled" {
		return infraerrors.BadRequest("INVALID_EMPLOYEE_STATUS", "employee status is invalid")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var currentVersion int64
	err = tx.QueryRowContext(ctx, `SELECT version FROM enterprise_employees WHERE enterprise_id = $1 AND id = $2 AND status <> 'terminated' FOR UPDATE`, enterpriseID, employeeID).Scan(&currentVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return errNotFound
	}
	if err != nil {
		return err
	}
	if currentVersion != expectedVersion {
		conflictErr := errEmployeeVersionConflict.WithMetadata(map[string]string{"current_version": fmt.Sprintf("%d", currentVersion)})
		if err := writeAuditEvent(ctx, tx, enterpriseID, "employee.update_rejected", "employee", &employeeID, map[string]any{
			"result":           "rejected",
			"reason":           "version_conflict",
			"employee_id":      employeeID,
			"expected_version": expectedVersion,
			"current_version":  currentVersion,
		}); err != nil {
			return conflictErr
		}
		if err := tx.Commit(); err != nil {
			return conflictErr
		}
		return conflictErr
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE enterprise_employees SET status = $1, department_id = $2,
		    disabled_at = CASE WHEN $1::varchar = 'disabled' THEN NOW() ELSE NULL END,
		    auth_version = CASE WHEN status IS DISTINCT FROM $1 THEN auth_version + 1 ELSE auth_version END,
		    version = version + 1,
		    updated_at = NOW()
		WHERE enterprise_id = $3 AND id = $4 AND version = $5
		  AND ($2::bigint IS NULL OR EXISTS (SELECT 1 FROM enterprise_departments WHERE enterprise_id = $3 AND id = $2 AND status = 'active'))
	`, status, departmentID, enterpriseID, employeeID, expectedVersion)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		// The employee row is locked and its version already matched above, so
		// the only remaining condition guarded by the UPDATE's WHERE clause is
		// the department validity check: department_id must be NULL or point
		// to an active department belonging to this enterprise.
		return errEmployeeDepartmentInvalid
	}
	var revokedKeys []string
	if status == "disabled" {
		revokedKeys, err = revokeEmployeeActiveKeys(ctx, tx, enterpriseID, employeeID, "enterprise_employee_disabled")
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE enterprise_sessions SET revoked_at = COALESCE(revoked_at, NOW()), updated_at = NOW() WHERE enterprise_id = $1 AND principal_type = 'employee' AND principal_id = $2`, enterpriseID, employeeID)
		if err != nil {
			return err
		}
	}
	if err := writeAuditEvent(ctx, tx, enterpriseID, "employee.updated", "employee", &employeeID, map[string]any{"result": "success", "employee_id": employeeID, "status": status, "department_id": departmentID, "version": expectedVersion + 1}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.invalidateAuthCacheByKeys(ctx, revokedKeys)
	return nil
}

func (s *Service) TerminateEmployee(ctx context.Context, enterpriseID, employeeID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `
		UPDATE enterprise_employees SET status = 'terminated', current_email = NULL, department_id = NULL,
		    disabled_at = COALESCE(disabled_at, NOW()), terminated_at = NOW(), auth_version = auth_version + 1, version = version + 1, updated_at = NOW()
		WHERE enterprise_id = $1 AND id = $2 AND status <> 'terminated'
	`, enterpriseID, employeeID)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return errNotFound
	}
	revokedKeys, err := revokeEmployeeActiveKeys(ctx, tx, enterpriseID, employeeID, "enterprise_employee_terminated")
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE enterprise_sessions SET revoked_at = COALESCE(revoked_at, NOW()), updated_at = NOW() WHERE enterprise_id = $1 AND principal_type = 'employee' AND principal_id = $2`, enterpriseID, employeeID); err != nil {
		return err
	}
	if err := writeAuditEvent(ctx, tx, enterpriseID, "employee.terminated", "employee", &employeeID, map[string]any{"result": "success", "employee_id": employeeID}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.invalidateAuthCacheByKeys(ctx, revokedKeys)
	return nil
}

func revokeEmployeeActiveKeys(ctx context.Context, tx *sql.Tx, enterpriseID, employeeID int64, actorRef string) ([]string, error) {
	type activeKey struct {
		id  int64
		key string
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT assignment.api_key_id, api_key.key
		FROM enterprise_key_assignments AS assignment
		JOIN api_keys AS api_key ON api_key.id = assignment.api_key_id
		WHERE assignment.enterprise_id = $1 AND assignment.employee_id = $2 AND assignment.status = 'active'
		ORDER BY assignment.api_key_id`, enterpriseID, employeeID)
	if err != nil {
		return nil, err
	}
	keys := make([]activeKey, 0, 1)
	for rows.Next() {
		var key activeKey
		if err := rows.Scan(&key.id, &key.key); err != nil {
			_ = rows.Close()
			return nil, err
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	revokedKeys := make([]string, 0, len(keys))
	for _, key := range keys {
		if err := lockEnterpriseKeyCredential(ctx, tx, key.key); err != nil {
			return nil, err
		}
		var lockedKey string
		if err := tx.QueryRowContext(ctx, `
			SELECT api_key.key
			FROM enterprise_key_assignments AS assignment
			JOIN api_keys AS api_key ON api_key.id = assignment.api_key_id
			WHERE assignment.enterprise_id = $1
			  AND assignment.employee_id = $2
			  AND assignment.api_key_id = $3
			  AND assignment.status = 'active'
			  AND api_key.key = $4
			FOR UPDATE OF assignment, api_key`, enterpriseID, employeeID, key.id, key.key).Scan(&lockedKey); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, errConflict
			}
			return nil, err
		}
		result, err := tx.ExecContext(ctx, `
			UPDATE enterprise_key_assignments
			SET status = 'revoked', ended_at = NOW(), revoked_at = NOW(), actor_ref = $3, updated_at = NOW()
			WHERE enterprise_id = $1 AND employee_id = $2 AND api_key_id = $4 AND status = 'active'`,
			enterpriseID, employeeID, actorRef, key.id)
		if err != nil {
			return nil, err
		}
		if affected, err := result.RowsAffected(); err != nil || affected != 1 {
			if err != nil {
				return nil, err
			}
			return nil, errConflict
		}
		result, err = tx.ExecContext(ctx, `
			UPDATE api_keys
			SET key = ':revoked:' || id::text, status = 'disabled', updated_at = NOW()
			WHERE id = $1`, key.id)
		if err != nil {
			return nil, err
		}
		if affected, err := result.RowsAffected(); err != nil || affected != 1 {
			if err != nil {
				return nil, err
			}
			return nil, errNotFound
		}
		revokedKeys = append(revokedKeys, key.key)
	}
	return revokedKeys, nil
}

func lockEnterpriseKeyCredential(ctx context.Context, tx *sql.Tx, credential string) error {
	if strings.TrimSpace(credential) == "" {
		return errConflict
	}
	fingerprint := enterprise.EmployeeKeyCredentialFingerprint(credential)
	_, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, fingerprint)
	return err
}

func (s *Service) invalidateAuthCacheByKeys(ctx context.Context, keys []string) {
	if s.authCacheInvalidator == nil {
		return
	}
	for _, key := range keys {
		s.authCacheInvalidator.InvalidateAuthCacheByKey(ctx, key)
	}
}

func (s *Service) GetBrand(ctx context.Context, enterpriseID int64) (*BrandInput, error) {
	brand := new(BrandInput)
	err := s.db.QueryRowContext(ctx, `
		SELECT COALESCE(NULLIF(BTRIM(branding.enterprise_name), ''), enterprise.name, ''), COALESCE(branding.title, ''),
		       COALESCE(branding.body, ''), COALESCE(branding.slogan, ''),
		       COALESCE(branding.background_object_key, ''), COALESCE(branding.background_content_type, ''),
		       COALESCE(branding.background_sha256, ''), COALESCE(branding.background_size_bytes, 0)
		FROM enterprises AS enterprise
		LEFT JOIN enterprise_branding AS branding ON branding.enterprise_id = enterprise.id
		WHERE enterprise.id = $1
	`, enterpriseID).Scan(&brand.EnterpriseName, &brand.Title, &brand.Body, &brand.Slogan,
		&brand.BackgroundObjectKey, &brand.BackgroundContentType, &brand.BackgroundSHA256, &brand.BackgroundSize)
	if err != nil {
		return nil, err
	}
	applyBrandDefaults(brand)
	return brand, nil
}

func (s *Service) PutBrand(ctx context.Context, enterpriseID int64, input BrandInput) (*BrandInput, error) {
	if err := validateBrand(input); err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO enterprise_branding (enterprise_id, enterprise_name, title, body, slogan)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (enterprise_id) DO UPDATE SET enterprise_name = EXCLUDED.enterprise_name,
			title = EXCLUDED.title, body = EXCLUDED.body, slogan = EXCLUDED.slogan, updated_at = NOW()
	`, enterpriseID, strings.TrimSpace(input.EnterpriseName), strings.TrimSpace(input.Title), strings.TrimSpace(input.Body), strings.TrimSpace(input.Slogan))
	if err != nil {
		return nil, err
	}
	if input.BackgroundURL == "" || input.BackgroundURL == defaultBrandBackgroundURL {
		if _, err = tx.ExecContext(ctx, `
			UPDATE enterprise_branding
			SET background_url = '', background_object_key = '', background_content_type = '', background_sha256 = '',
				background_size_bytes = 0, updated_at = NOW()
			WHERE enterprise_id = $1
		`, enterpriseID); err != nil {
			return nil, err
		}
	}
	if err := writeAuditEvent(ctx, tx, enterpriseID, "brand.updated", "enterprise_branding", &enterpriseID, map[string]any{"result": "success", "fields": []string{"enterprise_name", "title", "body", "slogan", "background"}}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetBrand(ctx, enterpriseID)
}

func (s *Service) UploadBrandBackground(ctx context.Context, enterpriseID int64, expectedSHA256 string, data []byte) (*BrandObject, error) {
	storage, ok := s.resolveBrandStorage()
	if !ok {
		return nil, errInvalidBrand
	}
	contentType, actualSHA256, extension, err := inspectBrandImage(data)
	if err != nil || !strings.EqualFold(strings.TrimSpace(expectedSHA256), actualSHA256) {
		return nil, errInvalidBrand
	}
	key := fmt.Sprintf("enterprises/%d/branding/background-%s%s", enterpriseID, actualSHA256, extension)
	if _, err := storage.Save(ctx, key, contentType, data); err != nil {
		return nil, err
	}
	writeBrand := func(q dbExecutor) error {
		_, err := q.ExecContext(ctx, `
			INSERT INTO enterprise_branding (enterprise_id, background_url, background_object_key, background_content_type, background_sha256, background_size_bytes)
		VALUES ($1, '', $2, $3, $4, $5)
		ON CONFLICT (enterprise_id) DO UPDATE SET background_url = '', background_object_key = EXCLUDED.background_object_key,
			background_content_type = EXCLUDED.background_content_type,
			background_sha256 = EXCLUDED.background_sha256,
			background_size_bytes = EXCLUDED.background_size_bytes, updated_at = NOW()
		`, enterpriseID, key, contentType, actualSHA256, int64(len(data)))
		return err
	}
	if actor := auditActor(ctx); actor == "" {
		if err := writeBrand(s.db); err != nil {
			return nil, err
		}
	} else {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return nil, err
		}
		defer tx.Rollback()
		if err := writeBrand(tx); err != nil {
			return nil, err
		}
		if err := writeAuditEvent(ctx, tx, enterpriseID, "brand.background_updated", "enterprise_branding", &enterpriseID, map[string]any{"result": "success", "content_type": contentType, "size_bytes": len(data)}); err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
	}
	return &BrandObject{URL: publicBrandBackgroundURL, ContentType: contentType, SHA256: actualSHA256, Size: int64(len(data))}, nil
}

func (s *Service) ReadBrandBackground(ctx context.Context, enterpriseID int64) ([]byte, string, error) {
	var key, expectedContentType, expectedSHA256 string
	var expectedSize int64
	if err := s.db.QueryRowContext(ctx, `SELECT background_object_key, background_content_type, background_sha256, background_size_bytes FROM enterprise_branding WHERE enterprise_id = $1`, enterpriseID).Scan(&key, &expectedContentType, &expectedSHA256, &expectedSize); err != nil {
		return nil, "", errNotFound
	}
	prefix := fmt.Sprintf("enterprises/%d/", enterpriseID)
	storage, ok := s.resolveBrandStorage()
	if !ok || key == "" || !strings.HasPrefix(key, prefix) {
		return nil, "", errNotFound
	}
	data, storedContentType, err := storage.Load(ctx, key)
	if err != nil {
		return nil, "", errNotFound
	}
	contentType, actualSHA256, _, err := inspectBrandImage(data)
	if err != nil || storedContentType != expectedContentType || contentType != expectedContentType || actualSHA256 != expectedSHA256 || int64(len(data)) != expectedSize {
		return nil, "", errNotFound
	}
	return data, contentType, nil
}

func (s *Service) resolveBrandStorage() (BrandObjectStorage, bool) {
	if s.brandStorage != nil {
		return s.brandStorage, true
	}
	if s.brandStorageResolver == nil {
		return nil, false
	}
	return s.brandStorageResolver()
}

func inspectBrandImage(data []byte) (string, string, string, error) {
	if len(data) == 0 || int64(len(data)) > maxBrandBackgroundBytes {
		return "", "", "", errInvalidBrand
	}
	contentType := strings.TrimSpace(strings.Split(http.DetectContentType(data), ";")[0])
	extension := ""
	switch contentType {
	case "image/jpeg":
		extension = ".jpg"
	case "image/png":
		extension = ".png"
	case "image/webp":
		extension = ".webp"
	default:
		return "", "", "", errInvalidBrand
	}
	imageConfig, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || imageConfig.Width <= 0 || imageConfig.Height <= 0 ||
		imageConfig.Width > maxBrandImageDimension || imageConfig.Height > maxBrandImageDimension ||
		int64(imageConfig.Width)*int64(imageConfig.Height) > maxBrandImagePixels {
		return "", "", "", errInvalidBrand
	}
	if _, _, err := image.Decode(bytes.NewReader(data)); err != nil {
		return "", "", "", errInvalidBrand
	}
	digest := sha256.Sum256(data)
	return contentType, hex.EncodeToString(digest[:]), extension, nil
}

func applyBrandDefaults(brand *BrandInput) {
	if strings.TrimSpace(brand.EnterpriseName) == "" {
		brand.EnterpriseName = defaultBrandEnterpriseName
	}
	if strings.TrimSpace(brand.Title) == "" {
		brand.Title = defaultBrandTitle
	}
	if strings.TrimSpace(brand.Body) == "" {
		brand.Body = defaultBrandBody
	}
	if strings.TrimSpace(brand.Slogan) == "" {
		brand.Slogan = defaultBrandSlogan
	}
	if brand.BackgroundObjectKey != "" {
		brand.BackgroundURL = publicBrandBackgroundURL
	} else {
		brand.BackgroundURL = defaultBrandBackgroundURL
		brand.BackgroundContentType = defaultBrandBackgroundContentType
		brand.BackgroundSHA256 = defaultBrandBackgroundSHA256
		brand.BackgroundSize = defaultBrandBackgroundSize
	}
}

func validateBrand(input BrandInput) error {
	input.EnterpriseName = strings.TrimSpace(input.EnterpriseName)
	if utf8.RuneCountInString(input.EnterpriseName) > 255 || utf8.RuneCountInString(input.Title) > 40 || utf8.RuneCountInString(input.Body) > 120 || utf8.RuneCountInString(input.Slogan) > 60 {
		return errInvalidBrand
	}
	if input.BackgroundURL != "" && input.BackgroundURL != publicBrandBackgroundURL && input.BackgroundURL != defaultBrandBackgroundURL {
		return errInvalidBrand
	}
	for _, value := range []string{input.EnterpriseName, input.Title, input.Body, input.Slogan} {
		lower := strings.ToLower(value)
		if strings.ContainsAny(value, "<>") || strings.Contains(lower, "script") || strings.Contains(lower, "svg") {
			return errInvalidBrand
		}
	}
	return nil
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
func tokenHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
func truncate(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return value[:max]
}
