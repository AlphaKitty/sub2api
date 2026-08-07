package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// CheckInHandler 签到赠送额度接口（需登录）。
type CheckInHandler struct {
	checkInService *service.CheckInService
}

// NewCheckInHandler 创建签到接口处理器。
func NewCheckInHandler(checkInService *service.CheckInService) *CheckInHandler {
	return &CheckInHandler{checkInService: checkInService}
}

// GetStatus 返回当前用户签到状态。
// GET /api/v1/user/check-in
func (h *CheckInHandler) GetStatus(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not found in context")
		return
	}

	status, err := h.checkInService.GetStatus(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, status)
}

// CheckIn 执行签到并发放奖励。
// POST /api/v1/user/check-in
func (h *CheckInHandler) CheckIn(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not found in context")
		return
	}

	result, err := h.checkInService.CheckIn(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}
