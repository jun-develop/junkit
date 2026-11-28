package validation

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/jun-develop/junkit/response"
)

// Errors the bind helpers answer with, exported so an app can list them in its API docs.
var (
	// ErrValidation is the 422 a failed bind answers; its "error" holds field -> message, or one
	// message when the body is not valid JSON at all.
	ErrValidation = response.New(response.UnprocessableEntity, "validation_error")
	// ErrInvalidUUID is ParseUUID's answer to a ":uuid" that is not a UUID.
	ErrInvalidUUID = response.New(response.BadRequest, "invalid_uuid")
)

// uuidParam binds the ":uuid" URI path parameter.
type uuidParam struct {
	UUID string `uri:"uuid" binding:"required,uuid"`
}

// BindJSON binds the JSON body into req, writing the 422 validation error response on failure.
//
// Usage:
//
//	var req CreateUserRequest
//	if !validation.BindJSON(ctx, h.responder, h.validator, &req) {
//		return
//	}
func BindJSON(ctx *gin.Context, responder response.Responder, validator *Validator, req any) bool {
	return bound(ctx, responder, validator, ctx.ShouldBindJSON(req))
}

// BindQuery binds the query string into req, writing the 422 validation error response on failure.
func BindQuery(ctx *gin.Context, responder response.Responder, validator *Validator, req any) bool {
	return bound(ctx, responder, validator, ctx.ShouldBindQuery(req))
}

// ParseUUID binds and validates the ":uuid" path parameter, writing the error response on failure.
//
// Usage:
//
//	uid, ok := validation.ParseUUID(ctx, h.responder, h.validator)
//	if !ok {
//		return
//	}
func ParseUUID(ctx *gin.Context, responder response.Responder, validator *Validator) (uuid.UUID, bool) {
	var params uuidParam
	if !bound(ctx, responder, validator, ctx.ShouldBindUri(&params)) {
		return uuid.Nil, false
	}

	uid, err := uuid.Parse(params.UUID)
	if err != nil {
		responder.Error(ctx, ErrInvalidUUID)
		return uuid.Nil, false
	}

	return uid, true
}

// bound reports whether a bind succeeded, answering 422 with the field errors when it did not.
func bound(ctx *gin.Context, responder response.Responder, validator *Validator, err error) bool {
	if err != nil {
		responder.SendWithData(ctx, ErrValidation.Code.HTTPStatus(), ErrValidation.Message, validator.GetValidationErrors(ctx, err))
		return false
	}

	return true
}
