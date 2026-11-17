package apidoc_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/jun-develop/junkit/apidoc"
	"github.com/jun-develop/junkit/pagination"
)

type createRequest struct {
	Name   string   `json:"name" binding:"required,min=2,max=50" example:"Alice"`
	Email  string   `json:"email" binding:"required,email,email_advanced"`
	Status *string  `json:"status" binding:"omitempty,oneof=active inactive"`
	Tags   []string `json:"tags" binding:"omitempty,max=3,dive,min=1"`
	IDs    []string `json:"ids" binding:"dive,uuid"`
}

type listQuery struct {
	pagination.Request
}

type itemResponse struct {
	UUID      uuid.UUID `json:"uuid"`
	CreatedAt time.Time `json:"created_at"`
	Note      *string   `json:"note,omitempty"`
	Secret    string    `json:"-"`
}

// obj is a JSON object decoded from the spec.
type obj = map[string]any

// testSpec builds and decodes the spec shared by the Spec tests.
func testSpec(t *testing.T) obj {
	t.Helper()

	raw, err := apidoc.Spec(apidoc.Info{Title: "Test", Version: "v1"}, []apidoc.Operation{
		{
			Route: apidoc.Route{Method: http.MethodPost, Path: "/api/v1/items", Summary: "Create",
				Body: createRequest{}, Status: http.StatusCreated, Response: itemResponse{}},
			Tag:      "items",
			Security: apidoc.Security{Bearer: true, Permissions: []string{"items:create"}},
		},
		{
			Route: apidoc.Route{Method: http.MethodGet, Path: "/api/v1/items", Summary: "List",
				Query: listQuery{}, Response: itemResponse{}, Paginated: true},
		},
		{
			Route:    apidoc.Route{Method: http.MethodDelete, Path: "/api/internal/items/:uuid", Summary: "Delete"},
			Security: apidoc.Security{APIKeyHeader: "X-Internal-Secret"},
		},
		{
			Route: apidoc.Route{Method: http.MethodPost, Path: "/api/v1/items/:id/files", Summary: "Upload",
				Files: []string{"file"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	var doc obj
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}

	return doc
}

// at walks doc through nested object keys.
func at(t *testing.T, doc obj, keys ...string) obj {
	t.Helper()

	for _, k := range keys {
		next, ok := doc[k].(obj)
		if !ok {
			t.Fatalf("no object at %q in %v", k, keys)
		}

		doc = next
	}

	return doc
}

// assertJSON fails when got and want do not marshal to the same JSON.
func assertJSON(t *testing.T, got, want any) {
	t.Helper()

	g, _ := json.Marshal(got)
	w, _ := json.Marshal(want)
	if string(g) != string(w) {
		t.Errorf("got %s, want %s", g, w)
	}
}

func TestSpec_Operations(t *testing.T) {
	doc := testSpec(t)
	create := at(t, doc, "paths", "/api/v1/items", "post")
	body := at(t, create, "requestBody", "content", "application/json", "schema")
	props := at(t, body, "properties")
	data := at(t, create, "responses", "201", "content", "application/json", "schema", "properties", "data")
	list := at(t, doc, "paths", "/api/v1/items", "get")
	del := at(t, doc, "paths", "/api/internal/items/{uuid}", "delete")
	upload := at(t, doc, "paths", "/api/v1/items/{id}/files", "post")

	listParams, _ := list["parameters"].([]any)
	if len(listParams) != 5 {
		t.Fatalf("query param count = %d, want 5", len(listParams))
	}

	tests := []struct {
		name string
		got  any
		want any
	}{
		{"required", body["required"], []string{"name", "email"}},
		{"name", props["name"], obj{"type": "string", "minLength": 2, "maxLength": 50, "example": "Alice"}},
		{"email", props["email"], obj{"type": "string", "format": "email", "description": "Validated by: email_advanced"}},
		{"status", props["status"], obj{"type": "string", "nullable": true, "enum": []string{"active", "inactive"}}},
		{"tags", props["tags"], obj{"type": "array", "maxItems": 3, "items": obj{"type": "string", "minLength": 1}}},
		{"ids", props["ids"], obj{"type": "array", "items": obj{"type": "string", "format": "uuid"}}},
		{"security", create["security"], []map[string][]string{{"bearerAuth": {}}}},
		{"x-permissions", create["x-permissions"], []string{"items:create"}},
		{"response", data["properties"], obj{
			"uuid":       obj{"type": "string", "format": "uuid"},
			"created_at": obj{"type": "string", "format": "date-time"},
			"note":       obj{"type": "string", "nullable": true},
		}},
		{"page_size", listParams[1], obj{"name": "page_size", "in": "query", "required": false,
			"schema": obj{"type": "integer", "minimum": 1, "maximum": 100, "example": 10}}},
		{"path param", del["parameters"], []obj{{"name": "uuid", "in": "path", "required": true,
			"schema": map[string]string{"type": "string", "format": "uuid"}}}},
		{"api key", del["security"], []map[string][]string{{"X-Internal-Secret": {}}}},
		{"multipart body", upload["requestBody"], obj{"required": true, "content": obj{
			"multipart/form-data": obj{"schema": obj{
				"type":       "object",
				"properties": obj{"file": obj{"type": "string", "format": "binary"}},
				"required":   []string{"file"},
			}},
		}}},
		{"multipart errors", len(at(t, upload, "responses")), 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertJSON(t, tt.got, tt.want)
		})
	}
}

func TestRegister_ServesPageAndSpec(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	spec := []byte(`{"openapi":"3.0.3"}`)
	apidoc.Register(r, "/docs", "My API", spec)

	tests := []struct {
		name        string
		path        string
		contentType string
		contains    string
	}{
		{"page", "/docs", "text/html; charset=utf-8", `{url: "/docs/openapi.json"`},
		{"page title", "/docs", "text/html; charset=utf-8", "<title>My API</title>"},
		{"spec", "/docs/openapi.json", "application/json", string(spec)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tt.path, nil))

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", w.Code)
			}

			if got := w.Header().Get("Content-Type"); got != tt.contentType {
				t.Errorf("Content-Type = %q, want %q", got, tt.contentType)
			}

			if !strings.Contains(w.Body.String(), tt.contains) {
				t.Errorf("body does not contain %q", tt.contains)
			}
		})
	}
}
