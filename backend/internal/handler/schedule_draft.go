package handler

import (
	"github.com/gin-gonic/gin"

	"github.com/gbschedule/gbschedule/internal/dto"
)

// SaveDraft godoc
// @Summary Save the current timetable as a named draft
// @Tags schedules
// @Accept json
// @Produce json
// @Param input body dto.SaveDraftRequest true "draft payload"
// @Success 201 {object} dto.Response
// @Router /api/v1/schedules/drafts [post]
func (h *ScheduleHandler) SaveDraft(c *gin.Context) {
	var req dto.SaveDraftRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		BadRequest(c, err.Error())
		return
	}
	draft, err := h.service.SaveDraft(c.Request.Context(), &req)
	if err != nil {
		Error(c, err)
		return
	}
	Created(c, draft)
}

// ListDrafts godoc
// @Summary List schedule drafts with pagination
// @Tags schedules
// @Produce json
// @Param page query int false "page"
// @Param page_size query int false "page size"
// @Success 200 {object} dto.Response
// @Router /api/v1/schedules/drafts [get]
func (h *ScheduleHandler) ListDrafts(c *gin.Context) {
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

// GetDraft godoc
// @Summary Get a schedule draft with its snapshot entries
// @Tags schedules
// @Produce json
// @Param id path int true "draft id"
// @Success 200 {object} dto.Response
// @Router /api/v1/schedules/drafts/{id} [get]
func (h *ScheduleHandler) GetDraft(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	draft, err := h.service.GetDraft(c.Request.Context(), id)
	if err != nil {
		Error(c, err)
		return
	}
	OK(c, draft)
}

// PublishDraft godoc
// @Summary Publish a draft to replace the current timetable
// @Tags schedules
// @Accept json
// @Produce json
// @Param id path int true "draft id"
// @Param input body dto.PublishDraftRequest true "publish payload"
// @Success 200 {object} dto.Response
// @Router /api/v1/schedules/drafts/{id}/publish [post]
func (h *ScheduleHandler) PublishDraft(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	var req dto.PublishDraftRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		BadRequest(c, err.Error())
		return
	}
	result, err := h.service.PublishDraft(c.Request.Context(), id, &req)
	if err != nil {
		Error(c, err)
		return
	}
	OK(c, result)
}
