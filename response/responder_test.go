package response_test

// =============================================================================
// TEST CASE OVERVIEW
// =============================================================================
//
// File: responder_test.go
// Purpose: Verify the JSON envelope the Responder writes: status, code, message, data, error and api_info.
// Type: Unit
//
// Test functions: 12
// Total scenarios: 41
//
// #   Test Case                                           Scenario                                       Expected Result
// 1   TestResponder_Send_WritesSuccessEnvelope            Send 200 with a message key                    200, status "success", translated message, api_info set
// 2   TestResponder_SendWithData_RoutesByStatusCode       2xx status                                     Payload in data, no error
// 3   TestResponder_SendWithData_RoutesByStatusCode       4xx status                                     Payload in error, no data
// 4   TestResponder_Pagination_WrapsItemsAndPagination    Items with pagination meta                     data holds "items" and "pagination"
// 5   TestResponder_Translate_FormatsArguments            Key with a %s argument                         Returns the formatted translation
// 6   TestResponder_Error_UsesAppErrorCodeAndMessage      AppError NotFound                              404, NOT_FOUND code, translated message
// 7   TestResponder_Error_OrdinaryErrorBecomesGeneric500  Plain (non-AppError) error                     500 and the raw error recorded on the gin context
// 8   TestResponder_Error_NilBecomesGeneric500            nil error                                      500
// 9   TestResponder_Error_RecordsAppErrorsWorthLogging    4xx without cause                              Not recorded on the gin context
// 10  TestResponder_Error_RecordsAppErrorsWorthLogging    4xx with cause                                 Recorded
// 11  TestResponder_Error_RecordsAppErrorsWorthLogging    5xx without cause                              Recorded
// 12  TestResponder_Error_RecordsAppErrorsWorthLogging    5xx with cause                                 Recorded
// 13  TestResponder_UsesTranslatorDefaultLanguage         Default language set to VI                     Message in Vietnamese
// 14  TestResponder_Error_MapsEveryErrorCode              BAD_REQUEST                                    400
// 15  TestResponder_Error_MapsEveryErrorCode              UNAUTHORIZED                                   401
// 16  TestResponder_Error_MapsEveryErrorCode              NOT_FOUND                                      404
// 17  TestResponder_Error_MapsEveryErrorCode              FORBIDDEN                                      403
// 18  TestResponder_Error_MapsEveryErrorCode              METHOD_NOT_ALLOWED                             405
// 19  TestResponder_Error_MapsEveryErrorCode              CONFLICT                                       409
// 20  TestResponder_Error_MapsEveryErrorCode              UNSUPPORTED_MEDIA_TYPE                         415
// 21  TestResponder_Error_MapsEveryErrorCode              PAYLOAD_TOO_LARGE                              413
// 22  TestResponder_Error_MapsEveryErrorCode              TOO_MANY_REQUESTS                              429
// 23  TestResponder_Error_MapsEveryErrorCode              UNPROCESSABLE_ENTITY                           422
// 24  TestResponder_Error_MapsEveryErrorCode              INTERNAL_SERVER_ERROR                          500
// 25  TestResponder_Error_MapsEveryErrorCode              Unrecognized code                              500
// 26  TestResponder_APIInfoReadsTraceMiddlewareContext    Trace ID, version, env, start time in context  api_info carries all four (latency computed)
// 27  TestResponder_EveryErrorResponseHasACode            Send 400                                       BAD_REQUEST code
// 28  TestResponder_EveryErrorResponseHasACode            Send 403                                       FORBIDDEN code
// 29  TestResponder_EveryErrorResponseHasACode            Send 404                                       NOT_FOUND code
// 30  TestResponder_EveryErrorResponseHasACode            Send 405                                       METHOD_NOT_ALLOWED code
// 31  TestResponder_EveryErrorResponseHasACode            Send 418 (unmapped 4xx)                        Falls back to BAD_REQUEST
// 32  TestResponder_EveryErrorResponseHasACode            Send 413                                       PAYLOAD_TOO_LARGE code
// 33  TestResponder_EveryErrorResponseHasACode            Send 503 (unmapped 5xx)                        Falls back to INTERNAL_SERVER_ERROR
// 34  TestResponder_EveryErrorResponseHasACode            SendWithData 422                               UNPROCESSABLE_ENTITY code
// 35  TestResponder_EveryErrorResponseHasACode            Send 401                                       UNAUTHORIZED code
// 36  TestResponder_EveryErrorResponseHasACode            Send 409                                       CONFLICT code
// 37  TestResponder_EveryErrorResponseHasACode            Send 415                                       UNSUPPORTED_MEDIA_TYPE code
// 38  TestResponder_EveryErrorResponseHasACode            Send 429                                       TOO_MANY_REQUESTS code
// 39  TestResponder_EveryErrorResponseHasACode            Error with a plain error                       INTERNAL_SERVER_ERROR code
// 40  TestResponder_EveryErrorResponseHasACode            Send 200                                       No code
// 41  TestResponder_EveryErrorResponseHasACode            SendWithData 201                               No code
//
// =============================================================================

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jun-develop/junkit/i18n"
	"github.com/jun-develop/junkit/logger"
	"github.com/jun-develop/junkit/pagination"
	"github.com/jun-develop/junkit/response"
)

// plainError is an ordinary error that is not an *AppError.
var plainError = &fakeError{"a plain, unstructured error"}

// fakeError is a minimal error type that is not an *AppError.
type fakeError struct{ msg string }

// Error implements error.
func (e *fakeError) Error() string { return e.msg }

// newResponderTestContext returns a recorder and a gin test context with a GET / request.
func newResponderTestContext() (*httptest.ResponseRecorder, *gin.Context) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	return w, c
}

// decodeResponse unmarshals the recorded body into a response.Response.
func decodeResponse(t *testing.T, w *httptest.ResponseRecorder) response.Response {
	t.Helper()

	var res response.Response
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to decode response body %q: %v", w.Body.String(), err)
	}

	return res
}

func TestResponder_Send_WritesSuccessEnvelope(t *testing.T) {
	responder := response.NewResponder(i18n.NewTranslator())
	w, c := newResponderTestContext()

	responder.Send(c, http.StatusOK, "operation_successful")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	res := decodeResponse(t, w)
	if res.Status != "success" {
		t.Fatalf("Status = %q, want %q", res.Status, "success")
	}

	if res.Message != "Operation successful" {
		t.Fatalf("Message = %q, want %q", res.Message, "Operation successful")
	}

	if res.APIInfo == nil {
		t.Fatal("APIInfo = nil, want it always populated")
	}
}

func TestResponder_SendWithData_RoutesByStatusCode(t *testing.T) {
	responder := response.NewResponder(i18n.NewTranslator())

	t.Run("2xx status puts the payload in data", func(t *testing.T) {
		w, c := newResponderTestContext()

		responder.SendWithData(c, http.StatusOK, "operation_successful", map[string]string{"id": "1"})

		res := decodeResponse(t, w)
		if res.Data == nil {
			t.Fatal("Data = nil, want the payload for a 2xx status")
		}

		if res.Error != nil {
			t.Fatalf("Error = %v, want nil for a 2xx status", res.Error)
		}
	})

	t.Run("4xx status puts the payload in error", func(t *testing.T) {
		w, c := newResponderTestContext()

		responder.SendWithData(c, http.StatusUnprocessableEntity, "validation_error", map[string]string{"email": "required"})

		res := decodeResponse(t, w)
		if res.Error == nil {
			t.Fatal("Error = nil, want the payload for a 4xx status")
		}

		if res.Data != nil {
			t.Fatalf("Data = %v, want nil for a 4xx status", res.Data)
		}
	})
}

func TestResponder_Pagination_WrapsItemsAndPagination(t *testing.T) {
	responder := response.NewResponder(i18n.NewTranslator())
	w, c := newResponderTestContext()
	meta := pagination.NewPagination(1, 10, 25)

	responder.Pagination(c, http.StatusOK, "operation_successful", []string{"a", "b"}, meta)

	var raw map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatalf("failed to decode response body %q: %v", w.Body.String(), err)
	}

	data, ok := raw["data"].(map[string]any)
	if !ok {
		t.Fatalf("data = %#v, want an object with items and pagination", raw["data"])
	}

	if _, ok := data["items"]; !ok {
		t.Fatal(`data["items"] is missing`)
	}

	if _, ok := data["pagination"]; !ok {
		t.Fatal(`data["pagination"] is missing`)
	}
}

func TestResponder_Translate_FormatsArguments(t *testing.T) {
	translator := i18n.NewTranslator()
	translator.AddTranslations(map[string]map[string]string{
		i18n.LangEN: {"greeting": "Hello, %s!"},
	})
	responder := response.NewResponder(translator)

	if got := responder.Translate("greeting", "Jane"); got != "Hello, Jane!" {
		t.Fatalf("Translate(greeting, Jane) = %q, want %q", got, "Hello, Jane!")
	}
}

func TestResponder_Error_UsesAppErrorCodeAndMessage(t *testing.T) {
	translator := i18n.NewTranslator()
	translator.AddTranslations(map[string]map[string]string{
		i18n.LangEN: {"user_not_found": "User not found"},
	})
	responder := response.NewResponder(translator)
	w, c := newResponderTestContext()

	responder.Error(c, response.New(response.NotFound, "user_not_found"))

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNotFound)
	}

	res := decodeResponse(t, w)
	if res.Code != response.NotFound {
		t.Fatalf("Code = %q, want %q", res.Code, response.NotFound)
	}

	if res.Message != "User not found" {
		t.Fatalf("Message = %q, want %q", res.Message, "User not found")
	}
}

func TestResponder_Error_OrdinaryErrorBecomesGeneric500(t *testing.T) {
	responder := response.NewResponder(i18n.NewTranslator())
	w, c := newResponderTestContext()

	responder.Error(c, plainError)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d for a non-AppError", w.Code, http.StatusInternalServerError)
	}

	if len(c.Errors) != 1 {
		t.Fatalf("gin context Errors = %v, want the raw error recorded for logging middleware", c.Errors)
	}
}

func TestResponder_Error_NilBecomesGeneric500(t *testing.T) {
	responder := response.NewResponder(i18n.NewTranslator())
	w, c := newResponderTestContext()

	responder.Error(c, nil)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d for a nil error", w.Code, http.StatusInternalServerError)
	}
}

func TestResponder_Error_RecordsAppErrorsWorthLogging(t *testing.T) {
	cause := errors.New("connection refused")
	responder := response.NewResponder(i18n.NewTranslator())

	tests := []struct {
		name       string
		err        error
		wantErrors int
	}{
		{"4xx without cause is not recorded", response.New(response.NotFound, "k"), 0},
		{"4xx with cause is recorded", response.Wrap(response.Conflict, "k", cause), 1},
		{"5xx without cause is recorded", response.New(response.Internal, "k"), 1},
		{"5xx with cause is recorded", response.Wrap(response.Internal, "k", cause), 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, c := newResponderTestContext()

			responder.Error(c, tt.err)

			if len(c.Errors) != tt.wantErrors {
				t.Fatalf("gin context Errors = %v, want %d recorded", c.Errors, tt.wantErrors)
			}

			if tt.wantErrors > 0 && !errors.Is(c.Errors[0].Err, tt.err) {
				t.Fatalf("recorded error = %v, want %v", c.Errors[0].Err, tt.err)
			}
		})
	}
}

func TestResponder_UsesTranslatorDefaultLanguage(t *testing.T) {
	translator := i18n.NewTranslator()
	translator.AddTranslations(map[string]map[string]string{
		i18n.LangVI: {"operation_successful": "Thành công"},
	})
	translator.SetDefaultLang(i18n.LangVI)

	got := response.NewResponder(translator).Translate("operation_successful")

	if got != "Thành công" {
		t.Fatalf("Translate() = %q, want %q (the default-language message)", got, "Thành công")
	}
}

func TestResponder_Error_MapsEveryErrorCode(t *testing.T) {
	responder := response.NewResponder(i18n.NewTranslator())

	// The error code doubles as the subtest name: it is the whole scenario.
	tests := []struct {
		code       response.ErrorCode
		wantStatus int
	}{
		{response.BadRequest, http.StatusBadRequest},
		{response.Unauthorized, http.StatusUnauthorized},
		{response.NotFound, http.StatusNotFound},
		{response.Forbidden, http.StatusForbidden},
		{response.MethodNotAllowed, http.StatusMethodNotAllowed},
		{response.Conflict, http.StatusConflict},
		{response.UnsupportedMediaType, http.StatusUnsupportedMediaType},
		{response.PayloadTooLarge, http.StatusRequestEntityTooLarge},
		{response.TooManyRequests, http.StatusTooManyRequests},
		{response.UnprocessableEntity, http.StatusUnprocessableEntity},
		{response.Internal, http.StatusInternalServerError},
		{response.ErrorCode("SOMETHING_UNRECOGNIZED"), http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(string(tt.code), func(t *testing.T) {
			w, c := newResponderTestContext()

			responder.Error(c, response.New(tt.code, "some_key"))

			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d for code %q", w.Code, tt.wantStatus, tt.code)
			}
		})
	}
}

func TestResponder_APIInfoReadsTraceMiddlewareContext(t *testing.T) {
	responder := response.NewResponder(i18n.NewTranslator())
	w, c := newResponderTestContext()
	c.Request = c.Request.WithContext(logger.WithTraceID(c.Request.Context(), "trace-123"))
	c.Set(response.VersionContextKey, "v1.2.3")
	c.Set(response.EnvContextKey, "production")
	c.Set(response.StartTimeContextKey, time.Now().Add(-50*time.Millisecond))

	responder.Send(c, http.StatusOK, "operation_successful")

	res := decodeResponse(t, w)
	if res.APIInfo == nil {
		t.Fatal("APIInfo = nil, want it populated")
	}

	if res.APIInfo.TraceID != "trace-123" {
		t.Errorf("APIInfo.TraceID = %q, want %q", res.APIInfo.TraceID, "trace-123")
	}

	if res.APIInfo.Version != "v1.2.3" {
		t.Errorf("APIInfo.Version = %q, want %q", res.APIInfo.Version, "v1.2.3")
	}

	if res.APIInfo.Env != "production" {
		t.Errorf("APIInfo.Env = %q, want %q", res.APIInfo.Env, "production")
	}

	if res.APIInfo.Latency == "" {
		t.Error("APIInfo.Latency is empty, want it computed when start_time is present")
	}
}

func TestResponder_EveryErrorResponseHasACode(t *testing.T) {
	responder := response.NewResponder(i18n.NewTranslator())

	tests := []struct {
		name   string
		status int
		write  func(c *gin.Context)
		want   response.ErrorCode
	}{
		{"Send 400", 400, func(c *gin.Context) { responder.Send(c, 400, "k") }, response.BadRequest},
		{"Send 403", 403, func(c *gin.Context) { responder.Send(c, 403, "k") }, response.Forbidden},
		{"Send 404", 404, func(c *gin.Context) { responder.Send(c, 404, "k") }, response.NotFound},
		{"Send 405", 405, func(c *gin.Context) { responder.Send(c, 405, "k") }, response.MethodNotAllowed},
		{"Send 418 falls back", 418, func(c *gin.Context) { responder.Send(c, 418, "k") }, response.BadRequest},
		{"Send 413", 413, func(c *gin.Context) { responder.Send(c, 413, "k") }, response.PayloadTooLarge},
		{"Send 503 falls back", 503, func(c *gin.Context) { responder.Send(c, 503, "k") }, response.Internal},
		{"SendWithData 422", 422, func(c *gin.Context) { responder.SendWithData(c, 422, "k", map[string]string{"f": "bad"}) }, response.UnprocessableEntity},
		{"Send 401", 401, func(c *gin.Context) { responder.Send(c, 401, "k") }, response.Unauthorized},
		{"Send 409", 409, func(c *gin.Context) { responder.Send(c, 409, "k") }, response.Conflict},
		{"Send 415", 415, func(c *gin.Context) { responder.Send(c, 415, "k") }, response.UnsupportedMediaType},
		{"Send 429", 429, func(c *gin.Context) { responder.Send(c, 429, "k") }, response.TooManyRequests},
		{"Error plain err", 500, func(c *gin.Context) { responder.Error(c, plainError) }, response.Internal},
		{"Send 200 has none", 200, func(c *gin.Context) { responder.Send(c, 200, "k") }, ""},
		{"SendWithData 201 has none", 201, func(c *gin.Context) { responder.SendWithData(c, 201, "k", 1) }, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, c := newResponderTestContext()

			tt.write(c)

			if w.Code != tt.status {
				t.Fatalf("status = %d, want %d", w.Code, tt.status)
			}

			if got := decodeResponse(t, w).Code; got != tt.want {
				t.Errorf("Code = %q, want %q", got, tt.want)
			}
		})
	}
}
