package admin

import (
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// AccountHealthPolicyHandler manages group account health policies.
type AccountHealthPolicyHandler struct {
	svc *service.AccountHealthPolicyService
}

func NewAccountHealthPolicyHandler(svc *service.AccountHealthPolicyService) *AccountHealthPolicyHandler {
	return &AccountHealthPolicyHandler{svc: svc}
}

// GetByGroup GET /admin/groups/:id/health-policy
func (h *AccountHealthPolicyHandler) GetByGroup(c *gin.Context) {
	groupID, ok := parsePositiveInt64Param(c, "id")
	if !ok {
		return
	}
	policy, err := h.svc.GetByGroup(c.Request.Context(), groupID)
	if err != nil {
		response.InternalError(c, err.Error())
		return
	}
	response.Success(c, policy)
}

// Upsert PUT /admin/groups/:id/health-policy
func (h *AccountHealthPolicyHandler) Upsert(c *gin.Context) {
	groupID, ok := parsePositiveInt64Param(c, "id")
	if !ok {
		return
	}
	var req service.AccountHealthPolicyUpsert
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	policy, err := h.svc.Upsert(c.Request.Context(), groupID, req)
	if err != nil {
		if isClientError(err) {
			response.BadRequest(c, err.Error())
			return
		}
		response.InternalError(c, err.Error())
		return
	}
	response.Success(c, policy)
}

// DeleteByGroup DELETE /admin/groups/:id/health-policy
func (h *AccountHealthPolicyHandler) DeleteByGroup(c *gin.Context) {
	groupID, ok := parsePositiveInt64Param(c, "id")
	if !ok {
		return
	}
	if err := h.svc.DeleteByGroup(c.Request.Context(), groupID); err != nil {
		response.InternalError(c, err.Error())
		return
	}
	response.Success(c, gin.H{"deleted": true})
}

// RunNow POST /admin/groups/:id/health-policy/run
func (h *AccountHealthPolicyHandler) RunNow(c *gin.Context) {
	groupID, ok := parsePositiveInt64Param(c, "id")
	if !ok {
		return
	}
	run, err := h.svc.RunNow(c.Request.Context(), groupID)
	if err != nil {
		if isClientError(err) {
			response.BadRequest(c, err.Error())
			return
		}
		response.InternalError(c, err.Error())
		return
	}
	response.Success(c, run)
}

// ListRuns GET /admin/groups/:id/health-policy/runs
func (h *AccountHealthPolicyHandler) ListRuns(c *gin.Context) {
	groupID, ok := parsePositiveInt64Param(c, "id")
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	runs, err := h.svc.ListRuns(c.Request.Context(), groupID, limit)
	if err != nil {
		response.InternalError(c, err.Error())
		return
	}
	response.Success(c, runs)
}

// GetRun GET /admin/account-health-runs/:runId
func (h *AccountHealthPolicyHandler) GetRun(c *gin.Context) {
	runID, ok := parsePositiveInt64Param(c, "runId")
	if !ok {
		return
	}
	run, err := h.svc.GetRun(c.Request.Context(), runID)
	if err != nil {
		if strings.Contains(err.Error(), "no rows") {
			response.NotFound(c, "run not found")
			return
		}
		response.InternalError(c, err.Error())
		return
	}
	response.Success(c, run)
}

func parsePositiveInt64Param(c *gin.Context, name string) (int64, bool) {
	id, err := strconv.ParseInt(c.Param(name), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "invalid "+name)
		return 0, false
	}
	return id, true
}

func isClientError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "invalid") ||
		strings.Contains(msg, "required") ||
		strings.Contains(msg, "disabled") ||
		strings.Contains(msg, "not configured") ||
		strings.Contains(msg, "already in progress") ||
		strings.Contains(msg, "not found")
}
