package handler

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/enterprise"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
)

type EnterpriseAllocationStore interface {
	SetAllocation(context.Context, enterprise.SetAllocationParams) (*enterprise.Allocation, error)
	GetAllocationUsageSummary(context.Context, enterprise.AllocationUsageSummaryQuery) (*enterprise.AllocationUsageSummary, error)
}

type EnterpriseAllocationHandler struct {
	store EnterpriseAllocationStore
}

func NewEnterpriseAllocationHandler(store *enterprise.Repository) *EnterpriseAllocationHandler {
	return &EnterpriseAllocationHandler{store: store}
}

func NewEnterpriseAllocationHandlerWithStore(store EnterpriseAllocationStore) *EnterpriseAllocationHandler {
	return &EnterpriseAllocationHandler{store: store}
}

type setEnterpriseAllocationRequest struct {
	EnterpriseID    int64  `json:"enterprise_id" binding:"required"`
	WindowType      string `json:"window_type" binding:"required"`
	WindowAnchor    string `json:"window_anchor" binding:"required"`
	Credit          string `json:"credit" binding:"required"`
	ExpectedVersion int64  `json:"expected_version"`
	Reason          string `json:"reason" binding:"required"`
}

func (h *EnterpriseAllocationHandler) Set(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	subscriptionID, err := strconv.ParseInt(c.Param("subscription_id"), 10, 64)
	if err != nil || subscriptionID <= 0 {
		response.BadRequest(c, "Invalid subscription ID")
		return
	}
	employeeID, err := strconv.ParseInt(c.Param("employee_id"), 10, 64)
	if err != nil || employeeID <= 0 {
		response.BadRequest(c, "Invalid employee ID")
		return
	}
	var req setEnterpriseAllocationRequest
	if err = c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request")
		return
	}
	anchor, err := time.Parse(time.RFC3339, req.WindowAnchor)
	if err != nil {
		response.BadRequest(c, "Invalid window anchor")
		return
	}
	if err = enterprise.ValidateAllocationWindow(req.WindowType, anchor); err != nil {
		response.BadRequest(c, "Invalid allocation window")
		return
	}
	if _, err = enterprise.NormalizeAmount(req.Credit); err != nil {
		response.BadRequest(c, "Invalid allocation credit")
		return
	}
	if req.Reason, err = enterprise.NormalizeAllocationReason(req.Reason); err != nil {
		response.BadRequest(c, "Invalid allocation reason")
		return
	}
	allocation, err := h.store.SetAllocation(c.Request.Context(), enterprise.SetAllocationParams{
		RequesterUserID: subject.UserID,
		EnterpriseID:    req.EnterpriseID, SubscriptionID: subscriptionID, EmployeeID: employeeID,
		WindowType: req.WindowType, WindowAnchor: anchor, Credit: req.Credit,
		ExpectedVersion: req.ExpectedVersion, Reason: req.Reason,
	})
	if err != nil {
		writeEnterpriseAllocationError(c, err)
		return
	}
	response.Success(c, allocation)
}

func (h *EnterpriseAllocationHandler) Summary(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	enterpriseID, err := strconv.ParseInt(c.Query("enterprise_id"), 10, 64)
	if err != nil || enterpriseID <= 0 {
		response.BadRequest(c, "Invalid enterprise ID")
		return
	}
	subscriptionID, err := strconv.ParseInt(c.Param("subscription_id"), 10, 64)
	if err != nil || subscriptionID <= 0 {
		response.BadRequest(c, "Invalid subscription ID")
		return
	}
	employeeID, err := strconv.ParseInt(c.Param("employee_id"), 10, 64)
	if err != nil || employeeID <= 0 {
		response.BadRequest(c, "Invalid employee ID")
		return
	}
	anchor, err := time.Parse(time.RFC3339, c.Query("window_anchor"))
	if err != nil {
		response.BadRequest(c, "Invalid window anchor")
		return
	}
	if err = enterprise.ValidateAllocationWindow(c.Query("window_type"), anchor); err != nil {
		response.BadRequest(c, "Invalid allocation window")
		return
	}
	summary, err := h.store.GetAllocationUsageSummary(c.Request.Context(), enterprise.AllocationUsageSummaryQuery{
		RequesterUserID: subject.UserID,
		EnterpriseID:    enterpriseID, SubscriptionID: subscriptionID, EmployeeID: employeeID,
		WindowType: c.Query("window_type"), WindowAnchor: anchor,
	})
	if err != nil {
		writeEnterpriseAllocationError(c, err)
		return
	}
	response.Success(c, summary)
}

func writeEnterpriseAllocationError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, enterprise.ErrEnterpriseAccessDenied):
		response.NotFound(c, "Allocation not found")
	case errors.Is(err, sql.ErrNoRows):
		response.NotFound(c, "Allocation not found")
	case errors.Is(err, enterprise.ErrInvalidAmount),
		errors.Is(err, enterprise.ErrInvalidWindowType),
		errors.Is(err, enterprise.ErrInvalidWindowAnchor),
		errors.Is(err, enterprise.ErrReasonRequired),
		errors.Is(err, enterprise.ErrUnsafeReason):
		response.BadRequest(c, "Invalid allocation request")
	case errors.Is(err, enterprise.ErrAllocationVersionConflict):
		response.Error(c, 409, err.Error())
	default:
		response.ErrorFrom(c, err)
	}
}
