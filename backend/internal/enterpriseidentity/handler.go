package enterpriseidentity

import (
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

const claimsContextKey = "enterprise_identity_claims"

type Handler struct{ service *Service }

func NewHandler(service *Service) *Handler { return &Handler{service: service} }

func (h *Handler) RegisterRoutes(v1 *gin.RouterGroup) {
	root := v1.Group("/enterprise")
	root.POST("/auth/login", h.login)
	root.POST("/auth/refresh", h.refresh)
	root.POST("/auth/logout", h.logout)
	root.POST("/auth/forgot-password", h.forgotPassword)
	root.POST("/auth/reset-password", h.resetPassword)
	root.GET("/brand", h.getPublicBrand)
	root.GET("/brand/background", h.getPublicBrandBackground)

	authenticated := root.Group("")
	authenticated.Use(h.authenticate())
	authenticated.POST("/password/first-change", h.changeInitialPassword)
	authenticated.GET("/sessions", h.listSessions)
	authenticated.GET("/sessions/:id", h.getSession)
	authenticated.DELETE("/sessions/:id", h.revokeSession)
	authenticated.DELETE("/sessions", h.revokeAllSessions)

	admin := authenticated.Group("/admin")
	admin.Use(requireEnterpriseAdmin)
	admin.GET("/departments", h.listDepartments)
	admin.POST("/departments", h.createDepartment)
	admin.DELETE("/departments/:id", h.deleteDepartment)
	admin.GET("/employees", h.listEmployees)
	admin.POST("/employees", h.createEmployee)
	admin.PATCH("/employees/:id", h.updateEmployee)
	admin.DELETE("/employees/:id", h.terminateEmployee)
	admin.GET("/brand", h.getBrand)
	admin.PUT("/brand", h.putBrand)
	admin.POST("/brand/background", h.uploadBrandBackground)
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
	if response.ErrorFrom(c, h.service.RequestReset(c.Request.Context(), requestHost(c.Request), req.Email, resetBaseURL, c.GetHeader("Accept-Language"))) {
		return
	}
	response.Success(c, gin.H{"success": true})
}

func (h *Handler) resetPassword(c *gin.Context) {
	var req resetRequest
	if !bind(c, &req) {
		return
	}
	if response.ErrorFrom(c, h.service.ResetPassword(c.Request.Context(), requestHost(c.Request), req.Token, req.Password)) {
		return
	}
	response.Success(c, gin.H{"success": true})
}

func (h *Handler) changeInitialPassword(c *gin.Context) {
	var req changePasswordRequest
	if !bind(c, &req) {
		return
	}
	if response.ErrorFrom(c, h.service.ChangeInitialPassword(c.Request.Context(), mustClaims(c), req.CurrentPassword, req.NewPassword)) {
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
	if response.ErrorFrom(c, h.service.RevokeSession(c.Request.Context(), mustClaims(c), c.Param("id"))) {
		return
	}
	response.Success(c, gin.H{"success": true})
}

func (h *Handler) revokeAllSessions(c *gin.Context) {
	if response.ErrorFrom(c, h.service.RevokeAllSessions(c.Request.Context(), mustClaims(c))) {
		return
	}
	response.Success(c, gin.H{"success": true})
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
	var req departmentRequest
	if !bind(c, &req) {
		return
	}
	item, err := h.service.CreateDepartment(c.Request.Context(), mustClaims(c).EnterpriseID, req.Name)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Created(c, item)
}
func (h *Handler) deleteDepartment(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	if response.ErrorFrom(c, h.service.DeleteDepartment(c.Request.Context(), mustClaims(c).EnterpriseID, id)) {
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
func (h *Handler) createEmployee(c *gin.Context) {
	var req employeeCreateRequest
	if !bind(c, &req) {
		return
	}
	item, err := h.service.CreateEmployee(c.Request.Context(), mustClaims(c).EnterpriseID, req.Email, req.InitialPassword, req.DepartmentID)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Created(c, item)
}
func (h *Handler) updateEmployee(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req employeeUpdateRequest
	if !bind(c, &req) {
		return
	}
	if response.ErrorFrom(c, h.service.UpdateEmployee(c.Request.Context(), mustClaims(c).EnterpriseID, id, req.Status, req.DepartmentID)) {
		return
	}
	response.Success(c, gin.H{"success": true})
}
func (h *Handler) terminateEmployee(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	if response.ErrorFrom(c, h.service.TerminateEmployee(c.Request.Context(), mustClaims(c).EnterpriseID, id)) {
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
	var req BrandInput
	if !bind(c, &req) {
		return
	}
	brand, err := h.service.PutBrand(c.Request.Context(), mustClaims(c).EnterpriseID, req)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, brand)
}

func (h *Handler) uploadBrandBackground(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBrandBackgroundBytes+(1<<20))
	fileHeader, err := c.FormFile("file")
	if err != nil || fileHeader.Size <= 0 || fileHeader.Size > maxBrandBackgroundBytes {
		response.ErrorFrom(c, errInvalidBrand)
		return
	}
	file, err := fileHeader.Open()
	if err != nil {
		response.ErrorFrom(c, errInvalidBrand)
		return
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, maxBrandBackgroundBytes+1))
	if err != nil || int64(len(data)) > maxBrandBackgroundBytes {
		response.ErrorFrom(c, errInvalidBrand)
		return
	}
	asset, err := h.service.UploadBrandBackground(c.Request.Context(), mustClaims(c).EnterpriseID, c.PostForm("sha256"), data)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, asset)
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
