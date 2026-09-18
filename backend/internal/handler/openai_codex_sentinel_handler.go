package handler

import "github.com/gin-gonic/gin"

func (h *OpenAIGatewayHandler) CodexSentinelStatus(c *gin.Context) {
	h.gatewayService.CodexSentinelStatus(c)
}
func (h *OpenAIGatewayHandler) CodexSentinelRefresh(c *gin.Context) {
	h.gatewayService.CodexSentinelRefresh(c)
}
