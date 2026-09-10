package admin

import (
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type CarpoolHandler struct{ service *service.CarpoolService }

func NewCarpoolHandler(s *service.CarpoolService) *CarpoolHandler { return &CarpoolHandler{service: s} }

type carpoolPreviewRequest struct {
	PlanID          int64                        `json:"plan_id" binding:"gte=0"`
	RenewFromTermID int64                        `json:"renew_from_term_id" binding:"omitempty,gt=0"`
	StartsAt        *time.Time                   `json:"starts_at"`
	Mode            string                       `json:"mode"`
	Takeover        *domain.CarpoolTakeoverInput `json:"takeover"`
}
type carpoolOpenRequest service.CarpoolOpenInput

func parseID(c *gin.Context, name string) (int64, bool) {
	id, err := strconv.ParseInt(c.Param(name), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid "+name)
		return 0, false
	}
	return id, true
}
func operationKey(c *gin.Context) (string, bool) {
	key := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if key == "" {
		response.BadRequest(c, "Idempotency-Key is required")
		return "", false
	}
	return key, true
}
func bindJSON(c *gin.Context, target any) bool {
	if err := c.ShouldBindJSON(target); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return false
	}
	return true
}

func (h *CarpoolHandler) Plans(c *gin.Context) {
	plans, err := h.service.Plans(c.Request.Context())
	if response.ErrorFrom(c, err) {
		return
	}
	out := make([]domain.CarpoolAdminPlan, 0, len(plans))
	for _, plan := range plans {
		out = append(out, plan.AdminProjection())
	}
	response.Success(c, out)
}
func (h *CarpoolHandler) Preview(c *gin.Context) {
	userID, ok := parseID(c, "id")
	if !ok {
		return
	}
	var req carpoolPreviewRequest
	if !bindJSON(c, &req) {
		return
	}
	var result *domain.CarpoolTermPreview
	var err error
	if req.RenewFromTermID > 0 {
		if (req.Mode != "" && req.Mode != "new") || req.Takeover != nil {
			response.BadRequest(c, "Renewal preview does not accept takeover details")
			return
		}
		result, err = h.service.PreviewRenewal(c.Request.Context(), userID, req.RenewFromTermID, req.PlanID)
	} else {
		if req.PlanID <= 0 {
			response.BadRequest(c, "plan_id is required for opening preview")
			return
		}
		result, err = h.service.Preview(c.Request.Context(), userID, req.PlanID, req.StartsAt, req.Mode, req.Takeover)
	}
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, result)
}
func (h *CarpoolHandler) Open(c *gin.Context) {
	userID, ok := parseID(c, "id")
	if !ok {
		return
	}
	key, ok := operationKey(c)
	if !ok {
		return
	}
	var req carpoolOpenRequest
	if !bindJSON(c, &req) {
		return
	}
	result, err := h.service.Open(c.Request.Context(), userID, getAdminIDFromContext(c), service.CarpoolOpenInput(req), key)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Created(c, result)
}
func (h *CarpoolHandler) Renew(c *gin.Context) {
	termID, ok := parseID(c, "id")
	if !ok {
		return
	}
	key, ok := operationKey(c)
	if !ok {
		return
	}
	var req service.CarpoolRenewInput
	if !bindJSON(c, &req) {
		return
	}
	result, err := h.service.Renew(c.Request.Context(), termID, getAdminIDFromContext(c), req, key)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Created(c, result)
}
func (h *CarpoolHandler) Terminate(c *gin.Context) {
	termID, ok := parseID(c, "id")
	if !ok {
		return
	}
	key, ok := operationKey(c)
	if !ok {
		return
	}
	var req service.CarpoolTerminateInput
	if !bindJSON(c, &req) {
		return
	}
	result, err := h.service.Terminate(c.Request.Context(), termID, getAdminIDFromContext(c), req, key)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, result)
}
func (h *CarpoolHandler) CreatePlanVersion(c *gin.Context) {
	planID, ok := parseID(c, "id")
	if !ok {
		return
	}
	key, ok := operationKey(c)
	if !ok {
		return
	}
	var req service.CarpoolPlanVersionInput
	if !bindJSON(c, &req) {
		return
	}
	result, err := h.service.CreatePlanVersion(c.Request.Context(), planID, getAdminIDFromContext(c), req, key)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Created(c, result.AdminProjection())
}
func (h *CarpoolHandler) AddPayment(c *gin.Context) {
	termID, ok := parseID(c, "id")
	if !ok {
		return
	}
	key, ok := operationKey(c)
	if !ok {
		return
	}
	var req domain.CarpoolPaymentInput
	if !bindJSON(c, &req) {
		return
	}
	result, err := h.service.RecordPayment(c.Request.Context(), termID, getAdminIDFromContext(c), req, key)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Created(c, result)
}
func (h *CarpoolHandler) Payments(c *gin.Context) {
	termID, ok := parseID(c, "id")
	if !ok {
		return
	}
	page, pageSize := response.ParsePagination(c)
	items, total, err := h.service.Payments(c.Request.Context(), termID, page, pageSize)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Paginated(c, items, total, page, pageSize)
}
func (h *CarpoolHandler) Adjust(c *gin.Context) {
	cycleID, ok := parseID(c, "id")
	if !ok {
		return
	}
	key, ok := operationKey(c)
	if !ok {
		return
	}
	var req service.CarpoolAdjustmentInput
	if !bindJSON(c, &req) {
		return
	}
	result, err := h.service.Adjust(c.Request.Context(), cycleID, getAdminIDFromContext(c), req, key)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, result)
}

func queryInt64(c *gin.Context, name string) (*int64, bool) {
	raw := c.Query(name)
	if raw == "" {
		return nil, true
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		response.BadRequest(c, "Invalid "+name)
		return nil, false
	}
	return &value, true
}
func queryInt(c *gin.Context, name string) (*int, bool) {
	raw := c.Query(name)
	if raw == "" {
		return nil, true
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		response.BadRequest(c, "Invalid "+name)
		return nil, false
	}
	return &value, true
}
func queryTime(c *gin.Context, name string) (*time.Time, bool) {
	raw := c.Query(name)
	if raw == "" {
		return nil, true
	}
	value, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		response.BadRequest(c, "Invalid "+name)
		return nil, false
	}
	return &value, true
}

func (h *CarpoolHandler) Terms(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)
	userID, ok := queryInt64(c, "user_id")
	if !ok {
		return
	}
	planID, ok := queryInt64(c, "plan_id")
	if !ok {
		return
	}
	from, ok := queryTime(c, "starts_from")
	if !ok {
		return
	}
	to, ok := queryTime(c, "starts_to")
	if !ok {
		return
	}
	items, total, err := h.service.Terms(c.Request.Context(), domain.CarpoolTermFilters{Page: page, PageSize: pageSize, UserID: userID, PlanID: planID, Status: c.Query("status"), StartsFrom: from, StartsTo: to})
	if response.ErrorFrom(c, err) {
		return
	}
	response.Paginated(c, items, total, page, pageSize)
}
func (h *CarpoolHandler) Cycles(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)
	userID, ok := queryInt64(c, "user_id")
	if !ok {
		return
	}
	termID, ok := queryInt64(c, "term_id")
	if !ok {
		return
	}
	cycleNo, ok := queryInt(c, "cycle_no")
	if !ok {
		return
	}
	from, ok := queryTime(c, "starts_from")
	if !ok {
		return
	}
	to, ok := queryTime(c, "starts_to")
	if !ok {
		return
	}
	items, total, err := h.service.Cycles(c.Request.Context(), domain.CarpoolCycleFilters{Page: page, PageSize: pageSize, UserID: userID, TermID: termID, CycleNo: cycleNo, State: c.Query("state"), StartsFrom: from, StartsTo: to})
	if response.ErrorFrom(c, err) {
		return
	}
	response.Paginated(c, items, total, page, pageSize)
}
func (h *CarpoolHandler) Ledger(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)
	userID, ok := queryInt64(c, "user_id")
	if !ok {
		return
	}
	termID, ok := queryInt64(c, "term_id")
	if !ok {
		return
	}
	cycleID, ok := queryInt64(c, "cycle_id")
	if !ok {
		return
	}
	items, total, err := h.service.Ledger(c.Request.Context(), domain.CarpoolLedgerFilters{Page: page, PageSize: pageSize, UserID: userID, TermID: termID, CycleID: cycleID, EventType: c.Query("event_type"), Bucket: c.Query("bucket")})
	if response.ErrorFrom(c, err) {
		return
	}
	response.Paginated(c, items, total, page, pageSize)
}

func (h *CarpoolHandler) BillingExceptions(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)
	userID, ok := queryInt64(c, "user_id")
	if !ok {
		return
	}
	termID, ok := queryInt64(c, "term_id")
	if !ok {
		return
	}
	cycleID, ok := queryInt64(c, "cycle_id")
	if !ok {
		return
	}
	items, total, err := h.service.BillingExceptions(c.Request.Context(), domain.CarpoolBillingExceptionFilters{
		Page: page, PageSize: pageSize, UserID: userID, TermID: termID, CycleID: cycleID, Status: c.Query("status"),
	})
	if response.ErrorFrom(c, err) {
		return
	}
	response.Paginated(c, items, total, page, pageSize)
}

func (h *CarpoolHandler) ReconcileBillingException(c *gin.Context) {
	billingRequestID, ok := parseID(c, "id")
	if !ok {
		return
	}
	key, ok := operationKey(c)
	if !ok {
		return
	}
	var req service.CarpoolBillingReconcileInput
	if !bindJSON(c, &req) {
		return
	}
	result, err := h.service.ReconcileBillingException(c.Request.Context(), billingRequestID, getAdminIDFromContext(c), req, key)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, result)
}
