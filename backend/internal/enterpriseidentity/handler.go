package enterpriseidentity

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/enterprise"
	"github.com/Wei-Shaw/sub2api/internal/middleware"
	ippkg "github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/redis/go-redis/v9"
)

const (
	claimsContextKey                       = "enterprise_identity_claims"
	enterpriseAuthRateLimitScopeContextKey = "enterprise_auth_rate_limit_scope"
	employeeKeyMutationRateLimitKey        = "enterprise-employee-key-mutation"
	employeeKeyMutationRateLimit           = 10
	employeeKeyMutationRateLimitWindow     = time.Minute
)

type Handler struct {
	service                    *Service
	keyRepository              employeeKeyStore
	apiKeyService              employeeKeyGenerator
	rateLimiter                *middleware.RateLimiter
	resolveRateLimitEnterprise func(context.Context, string) (int64, error)
	workbench                  *WorkbenchHandler
}

type employeeKeyStore interface {
	GetEmployeeCurrentKey(context.Context, int64, int64) (*enterprise.EmployeeKey, error)
	CreateEmployeeKey(context.Context, enterprise.EmployeeKeyMutationParams) (*enterprise.EmployeeKeyMutationResult, error)
	DisableEmployeeKey(context.Context, enterprise.EmployeeKeyMutationParams) (*enterprise.EmployeeKeyMutationResult, error)
	RotateEmployeeKey(context.Context, enterprise.EmployeeKeyMutationParams) (*enterprise.EmployeeKeyMutationResult, error)
}

type enterpriseAdminKeyStore interface {
	ListEnterpriseKeys(context.Context, int64) ([]enterprise.EnterpriseKeySummary, error)
	RevokeEnterpriseKey(context.Context, int64, int64, string, string) (*enterprise.EmployeeKeyMutationResult, error)
}

type employeeKeyGenerator interface {
	GenerateKey() (string, error)
}

func NewHandler(service *Service, keyRepository employeeKeyStore, apiKeyService employeeKeyGenerator, redisClient *redis.Client) *Handler {
	h := &Handler{service: service, keyRepository: keyRepository, apiKeyService: apiKeyService, rateLimiter: middleware.NewRateLimiter(redisClient)}
	h.workbench = NewWorkbenchHandler(h)
	if service != nil {
		h.resolveRateLimitEnterprise = func(ctx context.Context, host string) (int64, error) {
			enterprise, err := service.enterpriseByHost(ctx, host)
			if err != nil {
				return 0, err
			}
			return enterprise.ID, nil
		}
	}
	return h
}

func (h *Handler) RegisterRoutes(v1 *gin.RouterGroup) {
	root := v1.Group("/enterprise")
	h.registerPublicAuthRoute(root, "/auth/login", "enterprise-auth-login", 20, h.login)
	h.registerPublicAuthRoute(root, "/auth/refresh", "enterprise-auth-refresh", 30, h.refresh)
	root.POST("/auth/logout", h.logout)
	h.registerPublicAuthRoute(root, "/auth/forgot-password", "enterprise-auth-forgot-password", 5, h.forgotPassword)
	h.registerPublicAuthRoute(root, "/auth/reset-password", "enterprise-auth-reset-password", 10, h.resetPassword)
	root.GET("/brand", h.getPublicBrand)
	root.GET("/brand/background", h.getPublicBrandBackground)

	authenticated := root.Group("")
	authenticated.Use(h.authenticate())
	authenticated.POST("/password/first-change", h.changeInitialPassword)
	authenticated.POST("/password/change", h.changePassword)
	authenticated.GET("/sessions", h.listSessions)
	authenticated.GET("/sessions/:id", h.getSession)
	authenticated.DELETE("/sessions/:id", h.revokeSession)
	authenticated.DELETE("/sessions", h.revokeAllSessions)
	authenticated.GET("/keys/current", h.getCurrentKey)
	authenticated.POST("/keys", h.employeeKeyMutationRateLimit(), h.createKey)
	authenticated.POST("/keys/disable", h.employeeKeyMutationRateLimit(), h.disableKey)
	authenticated.POST("/keys/rotate", h.employeeKeyMutationRateLimit(), h.rotateKey)
	authenticated.GET("/home", h.employeeHome)
	authenticated.GET("/usage/me", h.employeeUsage)
	authenticated.GET("/usage/me/details", h.employeeUsageDetails)
	authenticated.GET("/profile", h.employeeProfile)

	admin := authenticated.Group("/admin")
	admin.Use(requireEnterpriseAdmin)
	admin.GET("/departments", h.listDepartments)
	admin.POST("/departments", h.createDepartment)
	admin.DELETE("/departments/:id", h.deleteDepartment)
	admin.GET("/employees", h.listEmployees)
	admin.GET("/employees/:id", h.getEmployee)
	admin.POST("/employees", h.createEmployee)
	admin.PATCH("/employees/:id", h.updateEmployee)
	admin.DELETE("/employees/:id", h.terminateEmployee)
	admin.GET("/brand", h.getBrand)
	admin.PUT("/brand", h.putBrand)
	admin.POST("/brand/background", h.uploadBrandBackground)
	admin.GET("/keys", h.listAdminKeys)
	admin.POST("/keys/:id/revoke", h.revokeAdminKey)
	if h.workbench != nil {
		h.workbench.registerRoutes(admin)
	}
}

func (h *Handler) registerPublicAuthRoute(root *gin.RouterGroup, path, key string, limit int, handler gin.HandlerFunc) {
	handlers := append(h.publicAuthRateLimits(key, limit), handler)
	root.POST(path, handlers...)
}

func (h *Handler) publicAuthRateLimits(key string, limit int) []gin.HandlerFunc {
	resolveEnterprise := func(c *gin.Context) {
		if h.resolveRateLimitEnterprise == nil {
			response.Error(c, http.StatusServiceUnavailable, "enterprise authentication is unavailable")
			c.Abort()
			return
		}
		enterpriseID, err := h.resolveRateLimitEnterprise(c.Request.Context(), requestHost(c.Request))
		if response.ErrorFrom(c, err) {
			c.Abort()
			return
		}
		c.Set(enterpriseAuthRateLimitScopeContextKey, enterpriseID)
		c.Next()
	}
	enterpriseClientLimit := h.rateLimiter.LimitWithOptions(key, limit, time.Minute, middleware.RateLimitOptions{
		FailureMode: middleware.RateLimitFailClose,
		KeyFunc: func(c *gin.Context, _ string) string {
			enterpriseID := c.GetInt64(enterpriseAuthRateLimitScopeContextKey)
			clientIP := ippkg.GetTrustedClientIP(c)
			return hashRateLimitScope(strconv.FormatInt(enterpriseID, 10) + "\x00" + clientIP)
		},
	})
	return []gin.HandlerFunc{resolveEnterprise, enterpriseClientLimit}
}

func (h *Handler) employeeKeyMutationRateLimit() gin.HandlerFunc {
	return h.rateLimiter.LimitWithOptions(
		employeeKeyMutationRateLimitKey,
		employeeKeyMutationRateLimit,
		employeeKeyMutationRateLimitWindow,
		middleware.RateLimitOptions{
			FailureMode: middleware.RateLimitFailClose,
			KeyFunc: func(c *gin.Context, _ string) string {
				claims := mustClaims(c)
				if claims == nil {
					return hashRateLimitScope("0\x000")
				}
				return hashRateLimitScope(
					strconv.FormatInt(claims.EnterpriseID, 10) + "\x00" + strconv.FormatInt(claims.PrincipalID, 10),
				)
			},
		},
	)
}

func hashRateLimitScope(scope string) string {
	digest := sha256.Sum256([]byte(scope))
	return fmt.Sprintf("%x", digest)
}

func (h *Handler) authenticate() gin.HandlerFunc {
	return func(c *gin.Context) {
		auth := strings.Fields(c.GetHeader("Authorization"))
		if len(auth) != 2 || !strings.EqualFold(auth[0], "Bearer") {
			response.Unauthorized(c, "Authorization header format must be 'Bearer {token}'")
			c.Abort()
			return
		}
		claims, _, err := h.service.Authenticate(c.Request.Context(), requestHost(c.Request), auth[1])
		if err != nil {
			response.ErrorFrom(c, err)
			c.Abort()
			return
		}
		if claims.ForceChange && !strings.HasSuffix(c.Request.URL.Path, "/password/first-change") {
			response.ErrorFrom(c, errForceChange)
			c.Abort()
			return
		}
		c.Set(claimsContextKey, claims)
		c.Next()
	}
}

func (h *Handler) EnterpriseAdminOrJWT(jwtAuth gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		auth := strings.Fields(c.GetHeader("Authorization"))
		if len(auth) != 2 || !strings.EqualFold(auth[0], "Bearer") || !looksLikeEnterpriseToken(auth[1]) {
			jwtAuth(c)
			return
		}

		claims, principal, err := h.service.Authenticate(c.Request.Context(), requestHost(c.Request), auth[1])
		if err != nil {
			response.ErrorFrom(c, err)
			c.Abort()
			return
		}
		if principal.ForceChange {
			response.ErrorFrom(c, errForceChange)
			c.Abort()
			return
		}

		c.Set(claimsContextKey, claims)
		c.Set(string(servermiddleware.ContextKeyUser), servermiddleware.AuthSubject{UserID: principal.PrincipalID})
		c.Set(string(servermiddleware.ContextKeyUserRole), principal.Role)
		c.Set(servermiddleware.ContextKeyAuthEmail, principal.Email)
		c.Set(servermiddleware.ContextKeySessionID, claims.SessionID)
		if principal.Role != "enterprise_admin" {
			response.Forbidden(c, "enterprise administrator permission is required")
			c.Abort()
			return
		}
		c.Next()
	}
}

func looksLikeEnterpriseToken(raw string) bool {
	if len(raw) > service.MaxTokenLength {
		return false
	}
	claims := new(Claims)
	if _, _, err := new(jwt.Parser).ParseUnverified(raw, claims); err != nil {
		return false
	}
	return claims.EnterpriseID > 0 && claims.PrincipalID > 0 && claims.SessionID != "" &&
		(claims.PrincipalType == "admin" || claims.PrincipalType == "employee")
}

func requireEnterpriseAdmin(c *gin.Context) {
	claims := mustClaims(c)
	if claims == nil || claims.Role != "enterprise_admin" {
		response.Forbidden(c, "enterprise administrator permission is required")
		c.Abort()
		return
	}
	c.Next()
}

func mustClaims(c *gin.Context) *Claims {
	value, ok := c.Get(claimsContextKey)
	if !ok {
		return nil
	}
	claims, _ := value.(*Claims)
	return claims
}

func ClaimsFromContext(c *gin.Context) (Claims, bool) {
	claims := mustClaims(c)
	if claims == nil {
		return Claims{}, false
	}
	return *claims, true
}

type loginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}
type refreshRequest struct {
	RefreshToken string `json:"refresh_token" binding:"required"`
}
type resetRequest struct {
	Token    string `json:"token" binding:"required"`
	Password string `json:"password" binding:"required"`
}
type forgotRequest struct {
	Email string `json:"email" binding:"required,email"`
}
type changePasswordRequest struct {
	CurrentPassword string `json:"current_password" binding:"required"`
	NewPassword     string `json:"new_password" binding:"required"`
}
type departmentRequest struct {
	Name string `json:"name" binding:"required"`
}
type employeeCreateRequest struct {
	Email           string `json:"email" binding:"required,email"`
	InitialPassword string `json:"initial_password" binding:"required"`
	DepartmentID    *int64 `json:"department_id"`
}
type employeeUpdateRequest struct {
	Status       string `json:"status" binding:"required"`
	DepartmentID *int64 `json:"department_id"`
}
type employeeKeyMutationRequest struct {
	ExpectedAPIKeyID int64 `json:"expected_api_key_id"`
}

type platformEnterpriseRequest struct {
	Name                  string `json:"name" binding:"required"`
	Host                  string `json:"portal_host" binding:"required"`
	DedicatedUpstreamUser int64  `json:"dedicated_upstream_user_id" binding:"required"`
	Reason                string `json:"reason" binding:"required"`
}

func (h *Handler) login(c *gin.Context) {
	var req loginRequest
	if !bind(c, &req) {
		return
	}
	pair, err := h.service.Login(c.Request.Context(), requestHost(c.Request), req.Email, req.Password, c.Request.UserAgent(), c.ClientIP())
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, pair)
}

func (h *Handler) CreatePlatformEnterprise(c *gin.Context) {
	subject, ok := servermiddleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	var req platformEnterpriseRequest
	if !bind(c, &req) {
		return
	}
	item, err := h.service.CreateEnterprise(c.Request.Context(), CreateEnterpriseInput{
		Name: req.Name, Host: req.Host, DedicatedUpstreamUser: req.DedicatedUpstreamUser, Reason: req.Reason,
	}, subject.UserID)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Created(c, item)
}

func (h *Handler) ListPlatformEnterprises(c *gin.Context) {
	items, err := h.service.ListPlatformEnterprises(c.Request.Context(), c.Query("search"), c.Query("status"))
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, items)
}

func (h *Handler) GetPlatformEnterprise(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	item, err := h.service.GetPlatformEnterprise(c.Request.Context(), id)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, item)
}

func (h *Handler) DisablePlatformEnterprise(c *gin.Context) {
	subject, ok := servermiddleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req struct {
		Reason string `json:"reason" binding:"required"`
	}
	if !bind(c, &req) {
		return
	}
	if response.ErrorFrom(c, h.service.DisableEnterprise(c.Request.Context(), id, subject.UserID, req.Reason)) {
		return
	}
	response.Success(c, gin.H{"success": true})
}

func (h *Handler) refresh(c *gin.Context) {
	var req refreshRequest
	if !bind(c, &req) {
		return
	}
	pair, err := h.service.Refresh(c.Request.Context(), requestHost(c.Request), req.RefreshToken, c.Request.UserAgent(), c.ClientIP())
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, pair)
}

func (h *Handler) logout(c *gin.Context) {
	var req refreshRequest
	if !bind(c, &req) {
		return
	}
	if response.ErrorFrom(c, h.service.Logout(c.Request.Context(), requestHost(c.Request), req.RefreshToken)) {
		return
	}
	response.Success(c, gin.H{"success": true})
}

func (h *Handler) forgotPassword(c *gin.Context) {
	var req forgotRequest
	if !bind(c, &req) {
		return
	}
	resetBaseURL := "https://" + requestHost(c.Request) + "/enterprise/reset-password"
	ctx := WithAuditActor(c.Request.Context(), "enterprise_public")
	if response.ErrorFrom(c, h.service.RequestReset(ctx, requestHost(c.Request), req.Email, resetBaseURL, c.GetHeader("Accept-Language"))) {
		return
	}
	response.Success(c, gin.H{"success": true})
}

func (h *Handler) resetPassword(c *gin.Context) {
	var req resetRequest
	if !bind(c, &req) {
		return
	}
	ctx := WithAuditActor(c.Request.Context(), "enterprise_public")
	if response.ErrorFrom(c, h.service.ResetPassword(ctx, requestHost(c.Request), req.Token, req.Password)) {
		return
	}
	response.Success(c, gin.H{"success": true})
}

func (h *Handler) changeInitialPassword(c *gin.Context) {
	var req changePasswordRequest
	if !bind(c, &req) {
		return
	}
	claims := mustClaims(c)
	ctx := WithAuditActor(c.Request.Context(), fmt.Sprintf("enterprise_%s:%d", claims.PrincipalType, claims.PrincipalID))
	if response.ErrorFrom(c, h.service.ChangeInitialPassword(ctx, claims, req.CurrentPassword, req.NewPassword)) {
		return
	}
	response.Success(c, gin.H{"success": true})
}

func (h *Handler) changePassword(c *gin.Context) {
	var req changePasswordRequest
	if !bind(c, &req) {
		return
	}
	claims := mustClaims(c)
	ctx := WithAuditActor(c.Request.Context(), fmt.Sprintf("enterprise_%s:%d", claims.PrincipalType, claims.PrincipalID))
	if response.ErrorFrom(c, h.service.ChangePassword(ctx, claims, req.CurrentPassword, req.NewPassword)) {
		return
	}
	response.Success(c, gin.H{"success": true})
}

func (h *Handler) listSessions(c *gin.Context) {
	items, err := h.service.ListSessions(c.Request.Context(), mustClaims(c))
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, items)
}

func (h *Handler) getSession(c *gin.Context) {
	items, err := h.service.ListSessions(c.Request.Context(), mustClaims(c))
	if response.ErrorFrom(c, err) {
		return
	}
	for _, item := range items {
		if item.ID == c.Param("id") {
			response.Success(c, item)
			return
		}
	}
	response.ErrorFrom(c, errNotFound)
}

func (h *Handler) revokeSession(c *gin.Context) {
	claims := mustClaims(c)
	ctx := WithAuditActor(c.Request.Context(), fmt.Sprintf("enterprise_%s:%d", claims.PrincipalType, claims.PrincipalID))
	if response.ErrorFrom(c, h.service.RevokeSession(ctx, claims, c.Param("id"))) {
		return
	}
	response.Success(c, gin.H{"success": true})
}

func (h *Handler) revokeAllSessions(c *gin.Context) {
	claims := mustClaims(c)
	ctx := WithAuditActor(c.Request.Context(), fmt.Sprintf("enterprise_%s:%d", claims.PrincipalType, claims.PrincipalID))
	if response.ErrorFrom(c, h.service.RevokeAllSessions(ctx, claims)) {
		return
	}
	response.Success(c, gin.H{"success": true})
}

func (h *Handler) getCurrentKey(c *gin.Context) {
	claims, ok := employeeClaims(c)
	if !ok {
		return
	}
	if h.keyRepository == nil {
		response.ErrorFrom(c, enterprise.ErrEmployeeKeyUnavailable)
		return
	}
	key, err := h.keyRepository.GetEmployeeCurrentKey(c.Request.Context(), claims.EnterpriseID, claims.PrincipalID)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, key)
}

func (h *Handler) employeeHome(c *gin.Context) {
	claims, ok := employeeClaims(c)
	if !ok {
		return
	}
	usage, err := h.service.GetEmployeeUsage(c.Request.Context(), claims.EnterpriseID, claims.PrincipalID)
	if response.ErrorFrom(c, err) {
		return
	}
	var key *enterprise.EmployeeKey
	if h.keyRepository != nil {
		key, err = h.keyRepository.GetEmployeeCurrentKey(c.Request.Context(), claims.EnterpriseID, claims.PrincipalID)
		if response.ErrorFrom(c, err) {
			return
		}
	}
	response.Success(c, gin.H{"usage": usage, "key": key})
}

func (h *Handler) employeeUsage(c *gin.Context) {
	claims, ok := employeeClaims(c)
	if !ok {
		return
	}
	usage, err := h.service.GetEmployeeUsage(c.Request.Context(), claims.EnterpriseID, claims.PrincipalID)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, usage)
}

func (h *Handler) employeeUsageDetails(c *gin.Context) {
	claims, ok := employeeClaims(c)
	if !ok {
		return
	}
	items, err := h.service.ListEmployeeUsage(c.Request.Context(), claims.EnterpriseID, claims.PrincipalID)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, items)
}

func (h *Handler) employeeProfile(c *gin.Context) {
	claims, ok := employeeClaims(c)
	if !ok {
		return
	}
	item, err := h.service.GetEmployee(c.Request.Context(), claims.EnterpriseID, claims.PrincipalID)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, item)
}

func (h *Handler) createKey(c *gin.Context) {
	h.mutateKey(c, "create")
}

func (h *Handler) disableKey(c *gin.Context) {
	h.mutateKey(c, "disable")
}

func (h *Handler) rotateKey(c *gin.Context) {
	h.mutateKey(c, "rotate")
}

func (h *Handler) mutateKey(c *gin.Context, operation string) {
	claims, ok := employeeClaims(c)
	if !ok {
		return
	}
	if h.keyRepository == nil || h.apiKeyService == nil {
		response.ErrorFrom(c, enterprise.ErrEmployeeKeyUnavailable)
		return
	}
	var req employeeKeyMutationRequest
	if operation != "create" && !bind(c, &req) {
		return
	}
	idempotencyKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if idempotencyKey == "" || len(idempotencyKey) > 128 {
		response.BadRequest(c, "Idempotency-Key header is required and must not exceed 128 characters")
		return
	}
	plaintext := ""
	if operation == "create" || operation == "rotate" {
		var err error
		plaintext, err = h.apiKeyService.GenerateKey()
		if response.ErrorFrom(c, err) {
			return
		}
	}
	params := enterprise.EmployeeKeyMutationParams{
		EnterpriseID: claims.EnterpriseID, EmployeeID: claims.PrincipalID,
		ExpectedAPIKeyID: req.ExpectedAPIKeyID, IdempotencyKey: idempotencyKey,
		Plaintext: plaintext, ActorRef: fmt.Sprintf("enterprise_%s:%d", claims.PrincipalType, claims.PrincipalID),
	}
	var result *enterprise.EmployeeKeyMutationResult
	var err error
	switch operation {
	case "create":
		result, err = h.keyRepository.CreateEmployeeKey(c.Request.Context(), params)
	case "disable":
		result, err = h.keyRepository.DisableEmployeeKey(c.Request.Context(), params)
	case "rotate":
		result, err = h.keyRepository.RotateEmployeeKey(c.Request.Context(), params)
	}
	if response.ErrorFrom(c, err) {
		return
	}
	if result == nil || result.Key == nil {
		response.ErrorFrom(c, enterprise.ErrEmployeeKeyUnavailable)
		return
	}
	if result.Replayed {
		result.Plaintext = ""
	}
	if result.Plaintext != "" {
		c.Header("Cache-Control", "no-store")
		c.Header("Pragma", "no-cache")
	}
	if operation == "create" && !result.Replayed {
		response.Created(c, result)
		return
	}
	response.Success(c, result)
}

func employeeClaims(c *gin.Context) (*Claims, bool) {
	claims := mustClaims(c)
	if claims == nil || claims.PrincipalType != "employee" || claims.Role != "enterprise_employee" {
		response.Forbidden(c, "enterprise employee permission is required")
		return nil, false
	}
	return claims, true
}

func (h *Handler) getPublicBrand(c *gin.Context) {
	e, err := h.service.enterpriseByHost(c.Request.Context(), requestHost(c.Request))
	if response.ErrorFrom(c, err) {
		return
	}
	brand, err := h.service.GetBrand(c.Request.Context(), e.ID)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, brand)
}

func (h *Handler) getPublicBrandBackground(c *gin.Context) {
	e, err := h.service.enterpriseByHost(c.Request.Context(), requestHost(c.Request))
	if response.ErrorFrom(c, err) {
		return
	}
	data, contentType, err := h.service.ReadBrandBackground(c.Request.Context(), e.ID)
	if response.ErrorFrom(c, err) {
		return
	}
	c.Header("Cache-Control", "public, max-age=300")
	c.Data(http.StatusOK, contentType, data)
}

func (h *Handler) listDepartments(c *gin.Context) {
	items, err := h.service.ListDepartments(c.Request.Context(), mustClaims(c).EnterpriseID)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, items)
}
func (h *Handler) createDepartment(c *gin.Context) {
	claims := mustClaims(c)
	ctx := WithAuditActor(c.Request.Context(), fmt.Sprintf("enterprise_%s:%d", claims.PrincipalType, claims.PrincipalID))
	var req departmentRequest
	if !bind(c, &req) {
		_ = h.service.RecordRejectedAuditEvent(ctx, claims.EnterpriseID, "department.create", "department", nil, "invalid_request")
		return
	}
	item, err := h.service.CreateDepartment(ctx, claims.EnterpriseID, req.Name)
	if response.ErrorFrom(c, err) {
		_ = h.service.RecordRejectedAuditEvent(ctx, claims.EnterpriseID, "department.create", "department", nil, "rejected")
		return
	}
	response.Created(c, item)
}
func (h *Handler) deleteDepartment(c *gin.Context) {
	claims := mustClaims(c)
	ctx := WithAuditActor(c.Request.Context(), fmt.Sprintf("enterprise_%s:%d", claims.PrincipalType, claims.PrincipalID))
	id, ok := pathID(c)
	if !ok {
		_ = h.service.RecordRejectedAuditEvent(ctx, claims.EnterpriseID, "department.disable", "department", nil, "invalid_request")
		return
	}
	if response.ErrorFrom(c, h.service.DeleteDepartment(ctx, claims.EnterpriseID, id)) {
		_ = h.service.RecordRejectedAuditEvent(ctx, claims.EnterpriseID, "department.disable", "department", &id, "rejected")
		return
	}
	response.Success(c, gin.H{"success": true})
}
func (h *Handler) listEmployees(c *gin.Context) {
	items, err := h.service.ListEmployees(c.Request.Context(), mustClaims(c).EnterpriseID)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, items)
}
func (h *Handler) getEmployee(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	claims := mustClaims(c)
	item, err := h.service.GetEmployee(c.Request.Context(), claims.EnterpriseID, id)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, item)
}
func (h *Handler) createEmployee(c *gin.Context) {
	claims := mustClaims(c)
	ctx := WithAuditActor(c.Request.Context(), fmt.Sprintf("enterprise_%s:%d", claims.PrincipalType, claims.PrincipalID))
	var req employeeCreateRequest
	if !bind(c, &req) {
		_ = h.service.RecordRejectedAuditEvent(ctx, claims.EnterpriseID, "employee.create", "employee", nil, "invalid_request")
		return
	}
	item, err := h.service.CreateEmployee(ctx, claims.EnterpriseID, req.Email, req.InitialPassword, req.DepartmentID)
	if response.ErrorFrom(c, err) {
		_ = h.service.RecordRejectedAuditEvent(ctx, claims.EnterpriseID, "employee.create", "employee", nil, "rejected")
		return
	}
	response.Created(c, item)
}
func (h *Handler) updateEmployee(c *gin.Context) {
	claims := mustClaims(c)
	ctx := WithAuditActor(c.Request.Context(), fmt.Sprintf("enterprise_%s:%d", claims.PrincipalType, claims.PrincipalID))
	id, ok := pathID(c)
	if !ok {
		_ = h.service.RecordRejectedAuditEvent(ctx, claims.EnterpriseID, "employee.update", "employee", nil, "invalid_request")
		return
	}
	var req employeeUpdateRequest
	if !bind(c, &req) {
		_ = h.service.RecordRejectedAuditEvent(ctx, claims.EnterpriseID, "employee.update", "employee", &id, "invalid_request")
		return
	}
	if response.ErrorFrom(c, h.service.UpdateEmployee(ctx, claims.EnterpriseID, id, req.Status, req.DepartmentID)) {
		_ = h.service.RecordRejectedAuditEvent(ctx, claims.EnterpriseID, "employee.update", "employee", &id, "rejected")
		return
	}
	response.Success(c, gin.H{"success": true})
}
func (h *Handler) terminateEmployee(c *gin.Context) {
	claims := mustClaims(c)
	ctx := WithAuditActor(c.Request.Context(), fmt.Sprintf("enterprise_%s:%d", claims.PrincipalType, claims.PrincipalID))
	id, ok := pathID(c)
	if !ok {
		_ = h.service.RecordRejectedAuditEvent(ctx, claims.EnterpriseID, "employee.terminate", "employee", nil, "invalid_request")
		return
	}
	if response.ErrorFrom(c, h.service.TerminateEmployee(ctx, claims.EnterpriseID, id)) {
		_ = h.service.RecordRejectedAuditEvent(ctx, claims.EnterpriseID, "employee.terminate", "employee", &id, "rejected")
		return
	}
	response.Success(c, gin.H{"success": true})
}
func (h *Handler) getBrand(c *gin.Context) {
	brand, err := h.service.GetBrand(c.Request.Context(), mustClaims(c).EnterpriseID)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, brand)
}
func (h *Handler) putBrand(c *gin.Context) {
	claims := mustClaims(c)
	ctx := WithAuditActor(c.Request.Context(), fmt.Sprintf("enterprise_%s:%d", claims.PrincipalType, claims.PrincipalID))
	var req BrandInput
	if !bind(c, &req) {
		_ = h.service.RecordRejectedAuditEvent(ctx, claims.EnterpriseID, "brand.update", "enterprise_branding", &claims.EnterpriseID, "invalid_request")
		return
	}
	brand, err := h.service.PutBrand(ctx, claims.EnterpriseID, req)
	if response.ErrorFrom(c, err) {
		_ = h.service.RecordRejectedAuditEvent(ctx, claims.EnterpriseID, "brand.update", "enterprise_branding", &claims.EnterpriseID, "rejected")
		return
	}
	response.Success(c, brand)
}

func (h *Handler) uploadBrandBackground(c *gin.Context) {
	claims := mustClaims(c)
	ctx := WithAuditActor(c.Request.Context(), fmt.Sprintf("enterprise_%s:%d", claims.PrincipalType, claims.PrincipalID))
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBrandBackgroundBytes+(1<<20))
	fileHeader, err := c.FormFile("file")
	if err != nil || fileHeader.Size <= 0 || fileHeader.Size > maxBrandBackgroundBytes {
		response.ErrorFrom(c, errInvalidBrand)
		h.recordRejectedAudit(ctx, claims.EnterpriseID, "brand.background.upload", "enterprise_branding", &claims.EnterpriseID, "invalid_request")
		return
	}
	file, err := fileHeader.Open()
	if err != nil {
		response.ErrorFrom(c, errInvalidBrand)
		h.recordRejectedAudit(ctx, claims.EnterpriseID, "brand.background.upload", "enterprise_branding", &claims.EnterpriseID, "rejected")
		return
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, maxBrandBackgroundBytes+1))
	if err != nil || int64(len(data)) > maxBrandBackgroundBytes {
		response.ErrorFrom(c, errInvalidBrand)
		h.recordRejectedAudit(ctx, claims.EnterpriseID, "brand.background.upload", "enterprise_branding", &claims.EnterpriseID, "invalid_request")
		return
	}
	asset, err := h.service.UploadBrandBackground(ctx, claims.EnterpriseID, c.PostForm("sha256"), data)
	if response.ErrorFrom(c, err) {
		h.recordRejectedAudit(ctx, claims.EnterpriseID, "brand.background.upload", "enterprise_branding", &claims.EnterpriseID, "rejected")
		return
	}
	response.Success(c, asset)
}

func (h *Handler) listAdminKeys(c *gin.Context) {
	store, ok := h.keyRepository.(enterpriseAdminKeyStore)
	if !ok {
		response.ErrorFrom(c, enterprise.ErrEmployeeKeyUnavailable)
		return
	}
	items, err := store.ListEnterpriseKeys(c.Request.Context(), mustClaims(c).EnterpriseID)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, items)
}

func (h *Handler) revokeAdminKey(c *gin.Context) {
	claims := mustClaims(c)
	ctx := WithAuditActor(c.Request.Context(), fmt.Sprintf("enterprise_%s:%d", claims.PrincipalType, claims.PrincipalID))
	store, ok := h.keyRepository.(enterpriseAdminKeyStore)
	if !ok {
		response.ErrorFrom(c, enterprise.ErrEmployeeKeyUnavailable)
		h.recordRejectedAudit(ctx, claims.EnterpriseID, "key.revoke", "api_key", nil, "rejected")
		return
	}
	id, ok := pathID(c)
	if !ok {
		h.recordRejectedAudit(ctx, claims.EnterpriseID, "key.revoke", "api_key", nil, "invalid_request")
		return
	}
	idempotencyKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if idempotencyKey == "" || len(idempotencyKey) > 128 {
		response.BadRequest(c, "Idempotency-Key header is required and must not exceed 128 characters")
		h.recordRejectedAudit(ctx, claims.EnterpriseID, "key.revoke", "api_key", &id, "invalid_request")
		return
	}
	result, err := store.RevokeEnterpriseKey(ctx, claims.EnterpriseID, id, idempotencyKey, auditActor(ctx))
	if response.ErrorFrom(c, err) {
		h.recordRejectedAudit(ctx, claims.EnterpriseID, "key.revoke", "api_key", &id, "rejected")
		return
	}
	if result != nil {
		result.Plaintext = ""
	}
	response.Success(c, result)
}

func (h *Handler) recordRejectedAudit(ctx context.Context, enterpriseID int64, eventType, entityType string, entityID *int64, reason string) {
	if h.service != nil {
		_ = h.service.RecordRejectedAuditEvent(ctx, enterpriseID, eventType, entityType, entityID, reason)
	}
}

func bind(c *gin.Context, value any) bool {
	if err := c.ShouldBindJSON(value); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return false
	}
	return true
}
func pathID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.Error(c, http.StatusBadRequest, "invalid id")
		return 0, false
	}
	return id, true
}
