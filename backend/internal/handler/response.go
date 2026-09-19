package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/gbschedule/gbschedule/internal/constants"
	"github.com/gbschedule/gbschedule/internal/dto"
	"github.com/gbschedule/gbschedule/internal/service"
)

// OK writes a unified success response.
func OK(c *gin.Context, data any) {
	c.JSON(http.StatusOK, dto.Response{Code: constants.CodeOK, Message: constants.MsgOK, Data: data})
}

// Created writes a unified created response.
func Created(c *gin.Context, data any) {
	c.JSON(http.StatusCreated, dto.Response{Code: constants.CodeOK, Message: constants.MsgOK, Data: data})
}

// Error converts a service error to a unified JSON response.
func Error(c *gin.Context, err error) {
	code, httpStatus, message := classifyError(err)
	c.JSON(httpStatus, dto.Response{Code: code, Message: message, Data: conflictData(err)})
}

func classifyError(err error) (int, int, string) {
	var detail *service.DetailError
	switch {
	case errors.As(err, &detail):
		switch {
		case errors.Is(err, service.ErrNotFound):
			return constants.CodeNotFound, http.StatusNotFound, detail.Detail
		case errors.Is(err, service.ErrConflict):
			return constants.CodeConflict, http.StatusConflict, detail.Detail
		default:
			return constants.CodeBadRequest, http.StatusBadRequest, detail.Detail
		}
	}

	var conflicts *service.ScheduleConflictsError
	if errors.As(err, &conflicts) {
		return constants.CodeConflict, http.StatusConflict, conflicts.Error()
	}

	switch {
	case errors.Is(err, service.ErrNotFound):
		return constants.CodeNotFound, http.StatusNotFound, constants.MsgNotFound
	case errors.Is(err, service.ErrInvalid):
		return constants.CodeBadRequest, http.StatusBadRequest, constants.MsgBadRequest
	case errors.Is(err, service.ErrConflict):
		return constants.CodeConflict, http.StatusConflict, constants.MsgConflict
	default:
		return constants.CodeInternal, http.StatusInternalServerError, constants.MsgInternal
	}
}

func conflictData(err error) any {
	var conflicts *service.ScheduleConflictsError
	if errors.As(err, &conflicts) {
		return gin.H{"conflicts": conflicts.Conflicts}
	}
	return nil
}

// BadRequest writes a 400 validation-style response.
func BadRequest(c *gin.Context, message string) {
	c.JSON(http.StatusBadRequest, dto.Response{Code: constants.CodeBadRequest, Message: message, Data: nil})
}

// parseID parses a uint path parameter.
func parseID(c *gin.Context, key string) (uint, bool) {
	raw := c.Param(key)
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || id == 0 {
		BadRequest(c, "invalid path parameter "+key)
		return 0, false
	}
	return uint(id), true
}
