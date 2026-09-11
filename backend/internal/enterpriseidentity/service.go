package enterpriseidentity

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	_ "golang.org/x/image/webp"

	"github.com/Wei-Shaw/sub2api/internal/config"
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
	errInvalidCredentials = infraerrors.Unauthorized("INVALID_CREDENTIALS", "invalid email or password")
	errInvalidToken       = infraerrors.Unauthorized("INVALID_ENTERPRISE_TOKEN", "invalid enterprise token")
	errInactive           = infraerrors.Unauthorized("ENTERPRISE_PRINCIPAL_INACTIVE", "enterprise or identity is not active")
	errWrongHost          = infraerrors.Unauthorized("ENTERPRISE_HOST_MISMATCH", "enterprise token is not valid for this host")
	errPasswordExpired    = infraerrors.Forbidden("INITIAL_PASSWORD_EXPIRED", "initial password has expired")
	errForceChange        = infraerrors.Forbidden("PASSWORD_CHANGE_REQUIRED", "password must be changed before continuing")
	errResetInvalid       = infraerrors.BadRequest("PASSWORD_RESET_INVALID", "password reset token is invalid or expired")
	errNotFound           = infraerrors.NotFound("ENTERPRISE_OBJECT_NOT_FOUND", "enterprise object not found")
	errConflict           = infraerrors.Conflict("ENTERPRISE_CONFLICT", "enterprise object conflicts with an existing record")
	errInvalidBrand       = infraerrors.BadRequest("INVALID_ENTERPRISE_BRAND", "enterprise brand content is invalid")
)

type PasswordResetMailer interface {
	SendEmail(ctx context.Context, to, subject, body string) error
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
	brandStorage         BrandObjectStorage
	brandStorageResolver func() (BrandObjectStorage, bool)
	now                  func() time.Time
}

type queryRower interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type Enterprise struct {
	ID          int64
	Name        string
	Host        string
	AdminUserID int64
	Status      string
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
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type Employee struct {
	ID           int64      `json:"id"`
	Email        string     `json:"email"`
	Status       string     `json:"status"`
	DepartmentID *int64     `json:"department_id,omitempty"`
	MustChange   bool       `json:"must_change_password"`
	TerminatedAt *time.Time `json:"terminated_at,omitempty"`
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

func NewService(db *sql.DB, cfg *config.Config, mailer *platformservice.EmailService, imageStorageSettings *platformservice.ImageStorageSettingService) *Service {
	secret := ""
	if cfg != nil {
		secret = cfg.JWT.Secret
	}
	service := &Service{db: db, secret: []byte(secret), now: time.Now}
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
		return nil, errInactive
	}
	return &e, nil
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
		return nil, errInactive
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
		return nil, errInactive
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
	if err != nil {
		return nil
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
	return nil
}

func (s *Service) ResetPassword(ctx context.Context, host, token, next string) error {
	if len(next) < 12 {
		return infraerrors.BadRequest("WEAK_PASSWORD", "password must be at least 12 characters")
	}
	e, err := s.enterpriseByHost(ctx, host)
	if err != nil {
		return errResetInvalid
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
	result, err := s.db.ExecContext(ctx, `
		UPDATE enterprise_sessions SET revoked_at = COALESCE(revoked_at, NOW()), updated_at = NOW()
		WHERE enterprise_id = $1 AND principal_type = $2 AND principal_id = $3 AND id = $4
	`, claims.EnterpriseID, claims.PrincipalType, claims.PrincipalID, id)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return errNotFound
	}
	return nil
}

func (s *Service) RevokeAllSessions(ctx context.Context, claims *Claims) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE enterprise_sessions SET revoked_at = COALESCE(revoked_at, NOW()), updated_at = NOW()
		WHERE enterprise_id = $1 AND principal_type = $2 AND principal_id = $3 AND revoked_at IS NULL
	`, claims.EnterpriseID, claims.PrincipalType, claims.PrincipalID)
	return err
}

func (s *Service) ListDepartments(ctx context.Context, enterpriseID int64) ([]Department, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name FROM enterprise_departments WHERE enterprise_id = $1 AND status = 'active' ORDER BY name`, enterpriseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Department, 0)
	for rows.Next() {
		var item Department
		if err := rows.Scan(&item.ID, &item.Name); err != nil {
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
	err := s.db.QueryRowContext(ctx, `INSERT INTO enterprise_departments (enterprise_id, name) VALUES ($1, $2) RETURNING id, name`, enterpriseID, name).Scan(&item.ID, &item.Name)
	if err != nil {
		return nil, errConflict
	}
	return item, nil
}

func (s *Service) DeleteDepartment(ctx context.Context, enterpriseID, departmentID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE enterprise_employees SET department_id = NULL, updated_at = NOW() WHERE enterprise_id = $1 AND department_id = $2`, enterpriseID, departmentID); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE enterprise_departments SET status = 'disabled', disabled_at = NOW(), updated_at = NOW() WHERE enterprise_id = $1 AND id = $2 AND status = 'active'`, enterpriseID, departmentID)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return errNotFound
	}
	return tx.Commit()
}

func (s *Service) ListEmployees(ctx context.Context, enterpriseID int64) ([]Employee, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, COALESCE(current_email, email), status, department_id, must_change_password, terminated_at FROM enterprise_employees WHERE enterprise_id = $1 ORDER BY id`, enterpriseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Employee, 0)
	for rows.Next() {
		var item Employee
		var dept sql.NullInt64
		var terminated sql.NullTime
		if err := rows.Scan(&item.ID, &item.Email, &item.Status, &dept, &item.MustChange, &terminated); err != nil {
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
	err = s.db.QueryRowContext(ctx, `
		INSERT INTO enterprise_employees (enterprise_id, email, current_email, password_hash, department_id, status, must_change_password, initial_password_expires_at)
		SELECT $1, $2, $2, $3, $4, 'active', TRUE, NOW() + INTERVAL '24 hours'
		WHERE $4::bigint IS NULL OR EXISTS (SELECT 1 FROM enterprise_departments WHERE enterprise_id = $1 AND id = $4 AND status = 'active')
		RETURNING id, current_email, status, department_id, must_change_password
	`, enterpriseID, email, string(hash), departmentID).Scan(&item.ID, &item.Email, &item.Status, &dept, &item.MustChange)
	if err != nil {
		return nil, errConflict
	}
	if dept.Valid {
		item.DepartmentID = &dept.Int64
	}
	return item, nil
}

func (s *Service) UpdateEmployee(ctx context.Context, enterpriseID, employeeID int64, status string, departmentID *int64) error {
	if status != "active" && status != "disabled" {
		return infraerrors.BadRequest("INVALID_EMPLOYEE_STATUS", "employee status is invalid")
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE enterprise_employees SET status = $1, department_id = $2,
		    disabled_at = CASE WHEN $1 = 'disabled' THEN NOW() ELSE NULL END,
		    auth_version = CASE WHEN status IS DISTINCT FROM $1 THEN auth_version + 1 ELSE auth_version END,
		    updated_at = NOW()
		WHERE enterprise_id = $3 AND id = $4 AND status <> 'terminated'
		  AND ($2::bigint IS NULL OR EXISTS (SELECT 1 FROM enterprise_departments WHERE enterprise_id = $3 AND id = $2 AND status = 'active'))
	`, status, departmentID, enterpriseID, employeeID)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return errNotFound
	}
	if status == "disabled" {
		_, err = s.db.ExecContext(ctx, `UPDATE enterprise_sessions SET revoked_at = COALESCE(revoked_at, NOW()), updated_at = NOW() WHERE enterprise_id = $1 AND principal_type = 'employee' AND principal_id = $2`, enterpriseID, employeeID)
	}
	return err
}

func (s *Service) TerminateEmployee(ctx context.Context, enterpriseID, employeeID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `
		UPDATE enterprise_employees SET status = 'terminated', current_email = NULL, department_id = NULL,
		    disabled_at = COALESCE(disabled_at, NOW()), terminated_at = NOW(), auth_version = auth_version + 1, updated_at = NOW()
		WHERE enterprise_id = $1 AND id = $2 AND status <> 'terminated'
	`, enterpriseID, employeeID)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return errNotFound
	}
	if _, err = tx.ExecContext(ctx, `UPDATE enterprise_sessions SET revoked_at = COALESCE(revoked_at, NOW()), updated_at = NOW() WHERE enterprise_id = $1 AND principal_type = 'employee' AND principal_id = $2`, enterpriseID, employeeID); err != nil {
		return err
	}
	return tx.Commit()
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
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO enterprise_branding (enterprise_id, background_url, background_object_key, background_content_type, background_sha256, background_size_bytes)
		VALUES ($1, '', $2, $3, $4, $5)
		ON CONFLICT (enterprise_id) DO UPDATE SET background_url = '', background_object_key = EXCLUDED.background_object_key,
			background_content_type = EXCLUDED.background_content_type,
			background_sha256 = EXCLUDED.background_sha256,
			background_size_bytes = EXCLUDED.background_size_bytes, updated_at = NOW()
	`, enterpriseID, key, contentType, actualSHA256, int64(len(data))); err != nil {
		return nil, err
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
