package apidoc_test

// =============================================================================
// TEST CASE OVERVIEW
// =============================================================================
//
// File: apidoc_test.go
// Purpose: Verify OpenAPI spec generation from routes and serving of the docs page and spec.
// Type: Unit
//
// Test functions: 2
// Total scenarios: 21
//
// #   Test Case                       Scenario                              Expected Result
// 1   TestSpec_DescribesOperations    Request body required fields          [name, email]
// 2   TestSpec_DescribesOperations    String with min/max and example tags  minLength 2, maxLength 50, example Alice
// 3   TestSpec_DescribesOperations    email + custom validator              format email; custom rule named in description
// 4   TestSpec_DescribesOperations    Optional oneof pointer                Nullable string enum
// 5   TestSpec_DescribesOperations    Slice with max and dive               Array with maxItems and item minLength
// 6   TestSpec_DescribesOperations    Slice with dive,uuid                  Array of uuid strings
// 7   TestSpec_DescribesOperations    Bearer security                       bearerAuth security requirement
// 8   TestSpec_DescribesOperations    Route permissions                     x-permissions lists them
// 9   TestSpec_DescribesOperations    Response struct                       uuid, date-time, nullable fields; json:"-" omitted
// 9a  TestSpec_DescribesOperations    Response required fields              Fields without omitempty/omitzero; slices not nullable
// 9b  TestSpec_DescribesOperations    Success envelope                      status, message, api_info, data required
// 9c  TestSpec_DescribesOperations    Paginated data                        items and pagination required
// 9d  TestSpec_DescribesOperations    Pagination and APIInfo components     Always-present fields required
// 10  TestSpec_DescribesOperations    Paginated query                       page_size param with 1..100 bounds (5 params in all)
// 11  TestSpec_DescribesOperations    Path :uuid                            Required uuid path parameter
// 12  TestSpec_DescribesOperations    API key header security               Security keyed by the header name
// 13  TestSpec_DescribesOperations    File upload route                     Required multipart/form-data body with binary file
// 14  TestSpec_DescribesOperations    File upload route                     Documents exactly 200 and 400 responses
// 15  TestRegister_ServesPageAndSpec  GET /docs                             HTML page loading /docs/openapi.json
// 16  TestRegister_ServesPageAndSpec  GET /docs                             HTML page titled with the API name
// 17  TestRegister_ServesPageAndSpec  GET /docs/openapi.json                The spec as JSON
//
// =============================================================================

import (
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
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
	Labels    []string  `json:"labels"`
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
		t.Fatalf("Spec() error = %v", err)
	}

	var doc obj
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("Spec() returned invalid JSON: %v", err)
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

	g, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal got: %v", err)
	}

	w, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal want: %v", err)
	}

	if string(g) != string(w) {
		t.Errorf("got %s, want %s", g, w)
	}
}

func TestSpec_DescribesOperations(t *testing.T) {
	doc := testSpec(t)
	create := at(t, doc, "paths", "/api/v1/items", "post")
	body := at(t, create, "requestBody", "content", "application/json", "schema")
	props := at(t, body, "properties")
	envelope := at(t, create, "responses", "201", "content", "application/json", "schema")
	data := at(t, envelope, "properties", "data")
	list := at(t, doc, "paths", "/api/v1/items", "get")
	del := at(t, doc, "paths", "/api/internal/items/{uuid}", "delete")
	upload := at(t, doc, "paths", "/api/v1/items/{id}/files", "post")

	listParams, ok := list["parameters"].([]any)
	if !ok {
		t.Fatalf("list parameters = %T, want a JSON array", list["parameters"])
	}

	if len(listParams) != 5 {
		t.Fatalf("query param count = %d, want 5", len(listParams))
	}

	tests := []struct {
		name string
		got  any
		want any
	}{
		{"body lists required fields", body["required"], []string{"name", "email"}},
		{"string with length limits and example", props["name"], obj{"type": "string", "minLength": 2, "maxLength": 50, "example": "Alice"}},
		{"email format with custom validator noted", props["email"], obj{"type": "string", "format": "email", "description": "Validated by: email_advanced"}},
		{"nullable enum", props["status"], obj{"type": "string", "nullable": true, "enum": []string{"active", "inactive"}}},
		{"array with item limits", props["tags"], obj{"type": "array", "maxItems": 3, "items": obj{"type": "string", "minLength": 1}}},
		{"array of uuids", props["ids"], obj{"type": "array", "items": obj{"type": "string", "format": "uuid"}}},
		{"bearer security", create["security"], []map[string][]string{{"bearerAuth": {}}}},
		{"required permissions", create["x-permissions"], []string{"items:create"}},
		{"response data schema", data["properties"], obj{
			"uuid":       obj{"type": "string", "format": "uuid"},
			"created_at": obj{"type": "string", "format": "date-time"},
			"note":       obj{"type": "string", "nullable": true},
			"labels":     obj{"type": "array", "items": obj{"type": "string"}},
		}},
		{"response requires fields its json tag never omits", data["required"], []string{"uuid", "created_at", "labels"}},
		{"success envelope required fields", envelope["required"], []string{"status", "message", "api_info", "data"}},
		{"paginated data required fields",
			at(t, list, "responses", "200", "content", "application/json", "schema", "properties", "data")["required"],
			[]string{"items", "pagination"}},
		{"pagination component required fields", at(t, doc, "components", "schemas", "Pagination")["required"],
			[]string{"page", "page_size", "total_items", "total_pages", "has_next", "has_prev", "next_page", "prev_page"}},
		{"api info component required fields", at(t, doc, "components", "schemas", "APIInfo")["required"], []string{"timestamp"}},
		{"pagination query parameter", listParams[1], obj{"name": "page_size", "in": "query", "required": false,
			"schema": obj{"type": "integer", "minimum": 1, "maximum": 100, "example": 10}}},
		{"path parameter", del["parameters"], []obj{{"name": "uuid", "in": "path", "required": true,
			"schema": map[string]string{"type": "string", "format": "uuid"}}}},
		{"api key header security", del["security"], []map[string][]string{{"X-Internal-Secret": {}}}},
		{"multipart file upload body", upload["requestBody"], obj{"required": true, "content": obj{
			"multipart/form-data": obj{"schema": obj{
				"type":       "object",
				"properties": obj{"file": obj{"type": "string", "format": "binary"}},
				"required":   []string{"file"},
			}},
		}}},
		{"multipart route response codes", slices.Sorted(maps.Keys(at(t, upload, "responses"))), []string{"200", "400"}},
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
		{"page loads the spec url", "/docs", "text/html; charset=utf-8", `{url: "/docs/openapi.json"`},
		{"page has the title", "/docs", "text/html; charset=utf-8", "<title>My API</title>"},
		{"spec is served as json", "/docs/openapi.json", "application/json", string(spec)},
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
				t.Errorf("body = %q, want it to contain %q", w.Body.String(), tt.contains)
			}
		})
	}
}
