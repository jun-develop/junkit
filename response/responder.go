package response

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/jun-develop/junkit/i18n"
	"github.com/jun-develop/junkit/pagination"
)

// Responder writes localized JSON responses in the Response envelope, resolving i18n keys in the
// translator's default language (i18n.Translator.SetDefaultLang).
//
// Usage:
//
//	responder.SendWithData(c, http.StatusOK, "user_fetched", user)
//	responder.Error(c, err)
type Responder interface {
	// Send writes a message-only response with key translated and formatted with args.
	Send(c *gin.Context, statusCode int, key string, args ...any)

	// SendWithData puts data in "data" for status < 400 and in "error" otherwise.
	SendWithData(c *gin.Context, statusCode int, key string, data any, args ...any)

	// Pagination writes data and its page metadata as PaginatedData in "data".
	Pagination(c *gin.Context, statusCode int, key string, data any, page pagination.Pagination, args ...any)

	// Translate resolves key to a localized string formatted with args.
	Translate(key string, args ...any) string

	// Error maps an *AppError to its Code's status and message, anything else to a generic 500.
	Error(c *gin.Context, err error)
}

// responder is the default Responder implementation.
type responder struct {
	translator *i18n.Translator
}

// NewResponder creates a Responder that translates messages with translator.
//
// Usage:
//
//	responder := response.NewResponder(translator)
func NewResponder(translator *i18n.Translator) Responder {
	return &responder{translator: translator}
}

// Send implements Responder.
func (r *responder) Send(c *gin.Context, statusCode int, key string, args ...any) {
	c.JSON(statusCode, Response{
		Status:  statusText(statusCode),
		Code:    httpStatusToErrorCode(statusCode),
		Message: r.Translate(key, args...),
		APIInfo: getAPIInfo(c),
	})
}

// SendWithData implements Responder.
func (r *responder) SendWithData(c *gin.Context, statusCode int, key string, data any, args ...any) {
	res := Response{
		Status:  statusText(statusCode),
		Code:    httpStatusToErrorCode(statusCode),
		Message: r.Translate(key, args...),
		APIInfo: getAPIInfo(c),
	}

	if statusCode >= http.StatusBadRequest {
		res.Error = data
	} else {
		res.Data = data
	}

	c.JSON(statusCode, res)
}

// Pagination implements Responder.
func (r *responder) Pagination(c *gin.Context, statusCode int, key string, data any, page pagination.Pagination, args ...any) {
	c.JSON(statusCode, Response{
		Status:  statusText(statusCode),
		Message: r.Translate(key, args...),
		Data: PaginatedData{
			Items:      data,
			Pagination: page,
		},
		APIInfo: getAPIInfo(c),
	})
}

// Translate implements Responder.
func (r *responder) Translate(key string, args ...any) string {
	// An empty language selects the translator's default language.
	return r.translator.GetMessage(key, "", args...)
}

// Error implements Responder. Errors worth logging (5xx or with a Cause) are recorded on the gin
// context so the access log sees them.
func (r *responder) Error(c *gin.Context, err error) {
	var appErr *AppError
	if errors.As(err, &appErr) {
		statusCode := errorCodeToHTTPStatus(appErr.Code)
		if statusCode >= http.StatusInternalServerError || appErr.Cause != nil {
			// The returned *gin.Error is only a handle to what was recorded.
			_ = c.Error(err)
		}

		c.JSON(statusCode, Response{
			Status:  "error",
			Code:    appErr.Code,
			Message: r.Translate(appErr.Message),
			APIInfo: getAPIInfo(c),
		})

		return
	}

	// gin's c.Error panics on nil; a nil err still gets the generic 500.
	if err != nil {
		// The returned *gin.Error is only a handle to what was recorded.
		_ = c.Error(err)
	}

	r.Send(c, http.StatusInternalServerError, "internal_server_error")
}

// errorCodeToHTTPStatus maps ErrorCode to HTTP status, defaulting to 500.
func errorCodeToHTTPStatus(code ErrorCode) int {
	switch code {
	case BadRequest:
		return http.StatusBadRequest
	case Unauthorized:
		return http.StatusUnauthorized
	case NotFound:
		return http.StatusNotFound
	case Forbidden:
		return http.StatusForbidden
	case MethodNotAllowed:
		return http.StatusMethodNotAllowed
	case Conflict:
		return http.StatusConflict
	case UnsupportedMediaType:
		return http.StatusUnsupportedMediaType
	case PayloadTooLarge:
		return http.StatusRequestEntityTooLarge
	case TooManyRequests:
		return http.StatusTooManyRequests
	case UnprocessableEntity:
		return http.StatusUnprocessableEntity
	case Internal:
		return http.StatusInternalServerError
	default:
		return http.StatusInternalServerError
	}
}

// httpStatusToErrorCode gives every error status (>= 400) a code so error bodies always carry one;
// unmapped statuses fall back to BAD_REQUEST (4xx) or INTERNAL_SERVER_ERROR (5xx).
func httpStatusToErrorCode(status int) ErrorCode {
	switch {
	case status < http.StatusBadRequest:
		return ""
	case status == http.StatusBadRequest:
		return BadRequest
	case status == http.StatusUnauthorized:
		return Unauthorized
	case status == http.StatusForbidden:
		return Forbidden
	case status == http.StatusNotFound:
		return NotFound
	case status == http.StatusMethodNotAllowed:
		return MethodNotAllowed
	case status == http.StatusConflict:
		return Conflict
	case status == http.StatusRequestEntityTooLarge:
		return PayloadTooLarge
	case status == http.StatusUnsupportedMediaType:
		return UnsupportedMediaType
	case status == http.StatusUnprocessableEntity:
		return UnprocessableEntity
	case status == http.StatusTooManyRequests:
		return TooManyRequests
	case status < http.StatusInternalServerError:
		return BadRequest
	default:
		return Internal
	}
}

// statusText returns "error" for status >= 400, "success" otherwise.
func statusText(code int) string {
	if code >= http.StatusBadRequest {
		return "error"
	}

	return "success"
}
