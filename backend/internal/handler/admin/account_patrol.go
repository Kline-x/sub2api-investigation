package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *AccountHandler) SetAccountPatrolService(patrol *service.AccountPatrolService) {
	h.accountPatrol = patrol
}

func (h *AccountHandler) GetAccountPatrolSettings(c *gin.Context) {
	if h.accountPatrol == nil {
		// Still return defaults so UI can render before wire is fully ready.
		response.Success(c, service.AccountPatrolSettings{
			Enabled:         false,
			IntervalMinutes: 30,
			BatchSize:       20,
			Concurrency:     4,
		})
		return
	}
	settings, err := h.accountPatrol.GetSettings(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, settings)
}

func (h *AccountHandler) UpdateAccountPatrolSettings(c *gin.Context) {
	if h.accountPatrol == nil {
		response.BadRequest(c, "account patrol service unavailable")
		return
	}
	var req service.AccountPatrolSettings
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if err := h.accountPatrol.UpdateSettings(c.Request.Context(), &req); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	settings, err := h.accountPatrol.GetSettings(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, settings)
}
