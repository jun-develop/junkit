package response_test

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

func TestResponder_Send(t *testing.T) {
	translator := i18n.NewTranslator()
	responder := response.NewResponder(translator)
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
		t.Fatalf("Message = %q, want the resolved translation", res.Message)
	}

	if res.APIInfo == nil {
		t.Fatal("APIInfo should always be populated")
	}
}

func TestResponder_SendWithData_RoutesByStatusCode(t *testing.T) {
	translator := i18n.NewTranslator()
	responder := response.NewResponder(translator)

	t.Run("success status uses the data field", func(t *testing.T) {
		w, c := newResponderTestContext()
		responder.SendWithData(c, http.StatusOK, "operation_successful", map[string]string{"id": "1"})

		res := decodeResponse(t, w)
		if res.Data == nil {
			t.Fatal("Data should be set for a 2xx status")
		}

		if res.Error != nil {
			t.Fatalf("Error = %v, want nil for a 2xx status", res.Error)
		}
	})

	t.Run("error status uses the error field", func(t *testing.T) {
		w, c := newResponderTestContext()
		responder.SendWithData(c, http.StatusUnprocessableEntity, "validation_error", map[string]string{"email": "required"})

		res := decodeResponse(t, w)
		if res.Error == nil {
			t.Fatal("Error should be set for a 4xx status")
		}

		if res.Data != nil {
			t.Fatalf("Data = %v, want nil for a 4xx status", res.Data)
		}
	})
}

func TestResponder_Pagination(t *testing.T) {
	translator := i18n.NewTranslator()
	responder := response.NewResponder(translator)
	w, c := newResponderTestContext()

	meta := pagination.NewPagination(1, 10, 25)
	responder.Pagination(c, http.StatusOK, "operation_successful", []string{"a", "b"}, meta)

	var raw map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	data, ok := raw["data"].(map[string]any)
	if !ok {
		t.Fatalf("data = %#v, want an object with items/pagination", raw["data"])
	}

	if _, ok := data["items"]; !ok {
		t.Fatal(`data["items"] missing`)
	}

	if _, ok := data["pagination"]; !ok {
		t.Fatal(`data["pagination"] missing`)
	}
}

func TestResponder_Translate(t *testing.T) {
	translator := i18n.NewTranslator()
	translator.AddTranslations(map[string]map[string]string{
		i18n.LangEN: {"greeting": "Hello, %s!"},
	})
	responder := response.NewResponder(translator)

	if got := responder.Translate("greeting", "Jane"); got != "Hello, Jane!" {
		t.Fatalf("Translate() = %q, want %q", got, "Hello, Jane!")
	}
}

func TestResponder_Error_AppError(t *testing.T) {
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
		t.Fatalf("Message = %q, want the resolved translation", res.Message)
	}
}

func TestResponder_Error_OrdinaryErrorBecomesGeneric500(t *testing.T) {
	translator := i18n.NewTranslator()
	responder := response.NewResponder(translator)
	w, c := newResponderTestContext()

	responder.Error(c, errors500Sentinel)

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

var errors500Sentinel = &fakeError{"a plain, unstructured error"}

// fakeError is a plain error that is not an *AppError.
type fakeError struct{ msg string }

// Error implements error.
func (e *fakeError) Error() string { return e.msg }

func TestResponder_Error_RecordsAppErrorsWorthLogging(t *testing.T) {
	cause := errors.New("connection refused")

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

	responder := response.NewResponder(i18n.NewTranslator())

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, c := newResponderTestContext()
			responder.Error(c, tt.err)

			if len(c.Errors) != tt.wantErrors {
				t.Fatalf("gin context Errors = %v, want %d recorded", c.Errors, tt.wantErrors)
			}

			if tt.wantErrors > 0 && !errors.Is(c.Errors[0].Err, tt.err) {
				t.Fatalf("recorded %v, want %v", c.Errors[0].Err, tt.err)
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

	if got := response.NewResponder(translator).Translate("operation_successful"); got != "Thành công" {
		t.Fatalf("Translate() = %q, want the default-language message", got)
	}
}

func TestResponder_Error_MapsEveryErrorCode(t *testing.T) {
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

	translator := i18n.NewTranslator()
	responder := response.NewResponder(translator)

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
	translator := i18n.NewTranslator()
	responder := response.NewResponder(translator)
	w, c := newResponderTestContext()

	c.Request = c.Request.WithContext(logger.WithTraceID(c.Request.Context(), "trace-123"))
	c.Set(response.VersionContextKey, "v1.2.3")
	c.Set(response.EnvContextKey, "production")
	c.Set(response.StartTimeContextKey, time.Now().Add(-50*time.Millisecond))

	responder.Send(c, http.StatusOK, "operation_successful")

	res := decodeResponse(t, w)
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
		t.Error("APIInfo.Latency should be computed when start_time is present")
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
		{"Error plain err", 500, func(c *gin.Context) { responder.Error(c, errors500Sentinel) }, response.Internal},
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
				t.Errorf("code = %q, want %q", got, tt.want)
			}
		})
	}
}
