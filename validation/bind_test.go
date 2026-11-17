package validation_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/jun-develop/junkit/i18n"
	"github.com/jun-develop/junkit/response"
	"github.com/jun-develop/junkit/validation"
)

// TestBind covers BindJSON, BindQuery and ParseUUID through a real gin router.
func TestBind(t *testing.T) {
	gin.SetMode(gin.TestMode)
	responder := response.NewResponder(i18n.NewTranslator())
	v := newValidator(t, validation.Options{})

	run := func(t *testing.T, path, target, body string, handle gin.HandlerFunc) *httptest.ResponseRecorder {
		t.Helper()
		r := gin.New()
		r.POST(path, handle)
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		return w
	}

	type body struct {
		Name string `json:"name" binding:"required"`
	}

	bindJSON := func(c *gin.Context) {
		var req body
		if validation.BindJSON(c, responder, v, &req) {
			c.Status(http.StatusNoContent)
		}
	}

	type query struct {
		Page int `form:"page" binding:"required,min=1"`
	}

	bindQuery := func(c *gin.Context) {
		var req query
		if validation.BindQuery(c, responder, v, &req) {
			c.Status(http.StatusNoContent)
		}
	}

	parseUUID := func(c *gin.Context) {
		if _, ok := validation.ParseUUID(c, responder, v); ok {
			c.Status(http.StatusNoContent)
		}
	}

	tests := []struct {
		name   string
		path   string
		target string
		body   string
		handle gin.HandlerFunc
		want   int
	}{
		{"BindJSON valid", "/", "/", `{"name":"a"}`, bindJSON, http.StatusNoContent},
		{"BindJSON invalid", "/", "/", `{}`, bindJSON, http.StatusUnprocessableEntity},
		{"BindQuery valid", "/", "/?page=2", "", bindQuery, http.StatusNoContent},
		{"BindQuery missing field", "/", "/", "", bindQuery, http.StatusUnprocessableEntity},
		{"BindQuery below minimum", "/", "/?page=0", "", bindQuery, http.StatusUnprocessableEntity},
		{"BindQuery not a number", "/", "/?page=abc", "", bindQuery, http.StatusUnprocessableEntity},
		{"ParseUUID valid", "/:uuid", "/0b6c4a8e-5d3f-4c1e-9a2b-7f8e9d0c1b2a", "", parseUUID, http.StatusNoContent},
		{"ParseUUID invalid", "/:uuid", "/not-a-uuid", "", parseUUID, http.StatusUnprocessableEntity},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if w := run(t, tt.path, tt.target, tt.body, tt.handle); w.Code != tt.want {
				t.Fatalf("status = %d, want %d (body %s)", w.Code, tt.want, w.Body.String())
			}
		})
	}
}
