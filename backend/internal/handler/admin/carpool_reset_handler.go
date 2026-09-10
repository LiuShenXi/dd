package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type CarpoolResetHandler struct {
	service *service.CarpoolResetService
}

func NewCarpoolResetHandler(resetService *service.CarpoolResetService) *CarpoolResetHandler {
	return &CarpoolResetHandler{service: resetService}
}

func (h *CarpoolResetHandler) Batches(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)
	scopeID, ok := queryInt64(c, "scope_id")
	if !ok {
		return
	}
	items, total, err := h.service.ListBatches(c.Request.Context(), domain.CarpoolResetBatchFilters{
		Page: page, PageSize: pageSize, ScopeID: scopeID, Status: c.Query("status"),
	})
	if response.ErrorFrom(c, err) {
		return
	}
	response.Paginated(c, items, total, page, pageSize)
}

func (h *CarpoolResetHandler) Register(c *gin.Context) {
	key, ok := operationKey(c)
	if !ok {
		return
	}
	var input service.CarpoolResetQualificationInput
	if !bindJSON(c, &input) {
		return
	}
	batch, err := h.service.RegisterQualification(c.Request.Context(), getAdminIDFromContext(c), input, key)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Created(c, batch)
}

func (h *CarpoolResetHandler) Schedule(c *gin.Context) {
	batchID, ok := parseID(c, "id")
	if !ok {
		return
	}
	key, ok := operationKey(c)
	if !ok {
		return
	}
	var input service.CarpoolResetScheduleInput
	if !bindJSON(c, &input) {
		return
	}
	batch, err := h.service.Schedule(c.Request.Context(), batchID, getAdminIDFromContext(c), input, key)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, batch)
}

func (h *CarpoolResetHandler) Execute(c *gin.Context) {
	batchID, ok := parseID(c, "id")
	if !ok {
		return
	}
	key, ok := operationKey(c)
	if !ok {
		return
	}
	var body struct{}
	if !bindJSON(c, &body) {
		return
	}
	batch, err := h.service.Execute(c.Request.Context(), batchID, getAdminIDFromContext(c), key)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, batch)
}

func (h *CarpoolResetHandler) Official(c *gin.Context) {
	key, ok := operationKey(c)
	if !ok {
		return
	}
	var input service.CarpoolOfficialResetInput
	if !bindJSON(c, &input) {
		return
	}
	batch, err := h.service.OfficialReset(c.Request.Context(), getAdminIDFromContext(c), input, key)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, batch)
}

func (h *CarpoolResetHandler) Observations(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)
	items, total, err := h.service.ListObservations(c.Request.Context(), domain.CarpoolResetObservationFilters{Page: page, PageSize: pageSize})
	if response.ErrorFrom(c, err) {
		return
	}
	response.Paginated(c, items, total, page, pageSize)
}

func (h *CarpoolResetHandler) ScanObservations(c *gin.Context) {
	key, ok := operationKey(c)
	if !ok {
		return
	}
	var body struct{}
	if !bindJSON(c, &body) {
		return
	}
	result, err := h.service.ScanObservations(c.Request.Context(), getAdminIDFromContext(c), key)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, result)
}
