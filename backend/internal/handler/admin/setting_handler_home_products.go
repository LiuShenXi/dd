package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// GetHomeProductsSettings 获取首页车次卡片配置
// GET /api/v1/admin/settings/home-products
func (h *SettingHandler) GetHomeProductsSettings(c *gin.Context) {
	settings, err := h.settingService.GetHomeProductsSettings(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, settings)
}

// UpdateHomeProductsSettings 更新首页车次卡片配置
// PUT /api/v1/admin/settings/home-products
func (h *SettingHandler) UpdateHomeProductsSettings(c *gin.Context) {
	var req service.HomeProductsSettings
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	if err := h.settingService.SetHomeProductsSettings(c.Request.Context(), &req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	updated, err := h.settingService.GetHomeProductsSettings(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, updated)
}
