package handler

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/gbschedule/gbschedule/internal/constants"
	"github.com/gbschedule/gbschedule/internal/dto"
	"github.com/gbschedule/gbschedule/internal/service"
)

// ScheduleDraftHandler handles timetable draft and publish endpoints.
type ScheduleDraftHandler struct {
	service service.ScheduleDraftService
	logger  *slog.Logger
}

// NewScheduleDraftHandler constructs a schedule draft handler.
func NewScheduleDraftHandler(service service.ScheduleDraftService, logger *slog.Logger) *ScheduleDraftHandler {
	return &ScheduleDraftHandler{service: service, logger: logger}
}

// Create godoc
// @Summary Save the current timetable as a named draft
// @Tags schedule-drafts
// @Accept json
// @Produce json
// @Param input body dto.CreateScheduleDraftRequest true "draft payload"
// @Success 201 {object} dto.Response
// @Failure 409 {object} dto.Response "draft name already exists"
// @Router /api/v1/schedule-drafts [post]
func (h *ScheduleDraftHandler) Create(c *gin.Context) {
	var req dto.CreateScheduleDraftRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		BadRequest(c, err.Error())
		return
	}
	result, err := h.service.SaveDraft(c.Request.Context(), &req)
	if err != nil {
		Error(c, err)
		return
	}
	c.JSON(http.StatusCreated, dto.Response{Code: constants.CodeOK, Message: constants.MsgOK, Data: result})
}

// List godoc
// @Summary List named timetable drafts
// @Tags schedule-drafts
// @Produce json
// @Param page query int false "page"
// @Param page_size query int false "page size"
// @Success 200 {object} dto.Response
// @Router /api/v1/schedule-drafts [get]
func (h *ScheduleDraftHandler) List(c *gin.Context) {
	var p dto.Pagination
	if err := c.ShouldBindQuery(&p); err != nil {
		BadRequest(c, "invalid pagination")
		return
	}
	p.Normalize()
	items, total, err := h.service.ListDrafts(c.Request.Context(), p.Page, p.PageSize)
	if err != nil {
		Error(c, err)
		return
	}
	OK(c, dto.PageData{Items: items, Total: total, Page: p.Page, PageSize: p.PageSize})
}

// Get godoc
// @Summary Get a draft with its full snapshot
// @Tags schedule-drafts
// @Produce json
// @Param id path int true "draft id"
// @Success 200 {object} dto.Response
// @Failure 404 {object} dto.Response "draft not found"
// @Router /api/v1/schedule-drafts/{id} [get]
func (h *ScheduleDraftHandler) Get(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	result, err := h.service.GetDraft(c.Request.Context(), id)
	if err != nil {
		Error(c, err)
		return
	}
	OK(c, result)
}

// Conflicts godoc
// @Summary Check conflicts inside a draft
// @Tags schedule-drafts
// @Produce json
// @Param id path int true "draft id"
// @Success 200 {object} dto.Response
// @Failure 404 {object} dto.Response "draft not found"
// @Router /api/v1/schedule-drafts/{id}/conflicts [get]
func (h *ScheduleDraftHandler) Conflicts(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	result, err := h.service.CheckDraftConflicts(c.Request.Context(), id)
	if err != nil {
		Error(c, err)
		return
	}
	OK(c, result)
}

// Publish godoc
// @Summary Publish a draft and atomically replace the current timetable
// @Tags schedule-drafts
// @Accept json
// @Produce json
// @Param id path int true "draft id"
// @Param input body dto.PublishScheduleDraftRequest true "publish payload"
// @Success 200 {object} dto.Response
// @Failure 404 {object} dto.Response "draft not found"
// @Failure 409 {object} dto.Response "conflicts present, already published or concurrent publish"
// @Router /api/v1/schedule-drafts/{id}/publish [post]
func (h *ScheduleDraftHandler) Publish(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	var req dto.PublishScheduleDraftRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		BadRequest(c, err.Error())
		return
	}
	result, err := h.service.Publish(c.Request.Context(), id, &req)
	if err != nil {
		Error(c, err)
		return
	}
	OK(c, result)
}

// Publishes godoc
// @Summary List publish audit records
// @Tags schedule-drafts
// @Produce json
// @Param draft_id query int false "filter by draft id"
// @Param page query int false "page"
// @Param page_size query int false "page size"
// @Success 200 {object} dto.Response
// @Router /api/v1/schedule-publishes [get]
func (h *ScheduleDraftHandler) Publishes(c *gin.Context) {
	var query struct {
		DraftID  uint `form:"draft_id" binding:"omitempty,gte=1"`
		Page     int  `form:"page" binding:"omitempty,min=1"`
		PageSize int  `form:"page_size" binding:"omitempty,min=1,max=200"`
	}
	if err := c.ShouldBindQuery(&query); err != nil {
		BadRequest(c, "invalid query parameters")
		return
	}
	p := dto.Pagination{Page: query.Page, PageSize: query.PageSize}
	p.Normalize()
	var draftID *uint
	if query.DraftID > 0 {
		draftID = &query.DraftID
	}
	items, total, err := h.service.ListPublishes(c.Request.Context(), draftID, p.Page, p.PageSize)
	if err != nil {
		Error(c, err)
		return
	}
	OK(c, dto.PageData{Items: items, Total: total, Page: p.Page, PageSize: p.PageSize})
}
