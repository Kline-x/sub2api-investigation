package admin

import (
	"strconv"

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

func (h *AccountHandler) ListAccountPatrolRecords(c *gin.Context) {
	if h.accountPatrol == nil {
		response.Success(c, gin.H{
			"items":     []any{},
			"total":     0,
			"page":      1,
			"page_size": 20,
		})
		return
	}
	page := 1
	pageSize := 20
	if v := c.Query("page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			page = n
		}
	}
	if v := c.Query("page_size"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			pageSize = n
		}
	}
	items, total, err := h.accountPatrol.ListRecords(c.Request.Context(), page, pageSize)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if items == nil {
		items = []service.AccountPatrolRecord{}
	}
	response.Success(c, gin.H{
		"items":     items,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}
