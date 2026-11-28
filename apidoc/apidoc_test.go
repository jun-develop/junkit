package apidoc_test

// =============================================================================
// TEST CASE OVERVIEW
// =============================================================================
//
// File: apidoc_test.go
// Purpose: Verify OpenAPI spec generation from routes and serving of the docs page and spec.
// Type: Unit
//
// Test functions: 4
// Total scenarios: 48
//
// #   Test Case                       Scenario                              Expected Result
// 1   TestSpec_DescribesOperations    Request body required fields          [name, email]
// 2   TestSpec_DescribesOperations    String with min/max and example tags  minLength 2, maxLength 50, example Alice
// 3   TestSpec_DescribesOperations    email + custom validator              format email, pattern, x-validation entry
// 4   TestSpec_DescribesOperations    Optional oneof pointer in a request   String enum, not nullable
// 5   TestSpec_DescribesOperations    Slice with max and dive               Array with maxItems and item minLength
// 6   TestSpec_DescribesOperations    Slice with dive,uuid                  Array of uuid strings
// 6a  TestSpec_DescribesOperations    Custom rule with a parameter          x-validation keeps rule and param, no pattern
// 6b  TestSpec_DescribesOperations    date rule                             format date
// 6c  TestSpec_DescribesOperations    Enum type slice in a request          Items $ref the type's enum
// 6g  TestSpec_DescribesOperations    String type without an enum           Plain string
// 6d  TestSpec_DescribesOperations    omitzero pointer with a pattern       anyOf the rule or "" (the validator skips "")
// 6e  TestSpec_DescribesOperations    omitempty pointer with min            Plain minLength ("" is validated, so rejected)
// 6f  TestSpec_DescribesOperations    omitempty plain string with min       anyOf the rule or ""
// 7   TestSpec_DescribesOperations    Bearer security                       bearerAuth security requirement
// 8   TestSpec_DescribesOperations    Route permissions                     x-permissions lists them
// 9   TestSpec_DescribesOperations    Response struct                       uuid, date-time, nullable, enum tag and enum type fields
// 9a  TestSpec_DescribesOperations    Response required fields              Fields without omitempty/omitzero; slices not nullable
// 9b  TestSpec_DescribesOperations    Success envelope                      status, message, api_info, data required
// 9c  TestSpec_DescribesOperations    Success envelope status               enum [success]
// 9d  TestSpec_DescribesOperations    Paginated data                        items and pagination required
// 9e  TestSpec_DescribesOperations    Pagination and APIInfo components     Always-present fields required
// 9f  TestSpec_DescribesOperations    Enum type                             Component schema named after the type
// 10  TestSpec_DescribesOperations    Paginated query                       page_size param with 1..100 bounds (5 params in all)
// 11  TestSpec_DescribesOperations    Path :uuid                            Required uuid path parameter
// 12  TestSpec_DescribesOperations    API key header security               Security keyed by the header name
// 13  TestSpec_DescribesOperations    File upload route                     Required multipart/form-data body with binary file
// 14  TestSpec_DescribesOperations    File upload route without Errors      Documents only its 200
// 15  TestSpec_DescribesOperations    operationId                           Each operation carries its ID
// 16  TestSpec_DescribesOperations    Route header                          Optional header parameter with description
// 17  TestSpec_DescribesOperations    Error status with one error kind      code and reason limited to its errors, all required
// 18  TestSpec_DescribesOperations    Errors of one status                  One response listing every reason, in order, once
// 19  TestSpec_DescribesOperations    422 with only validation              Variant with required "error" (map or string)
// 20  TestSpec_DescribesOperations    422 with validation and others        oneOf: validation variant, other reasons
// 21  TestSpec_DescribesOperations    Response codes of a body route        Success, 422 and each declared status
// 22  TestSpec_DescribesOperations    Error component                       All five fields required, status enum [error]
// 23  TestSpec_DescribesOperations    Error component                       code and reason enums of every error listed
// 24  TestSpec_DescribesOperations    Error component                       Optional "error" of map or string
// 25  TestSpec_Rejects                Operation without an ID               Error naming the route
// 26  TestSpec_Rejects                Two operations sharing an ID          Error naming the ID
// 27  TestSpec_Rejects                Two enums of one type name            Error naming the type
// 28  TestRegister_ServesPageAndSpec  GET /docs                             HTML page loading /docs/openapi.json
// 29  TestRegister_ServesPageAndSpec  GET /docs                             HTML page titled with the API name
// 30  TestRegister_ServesPageAndSpec  GET /docs/openapi.json                The spec as JSON
// 31  TestSpec_TypesPathParameters    :family_uuid                          uuid string
// 32  TestSpec_TypesPathParameters    :id                                   integer
// 33  TestSpec_TypesPathParameters    :slug                                 plain string
//
// =============================================================================

import (
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/jun-develop/junkit/apidoc"
	"github.com/jun-develop/junkit/pagination"
	"github.com/jun-develop/junkit/response"
	"github.com/jun-develop/junkit/validation"
)

type createRequest struct {
	Name   string   `json:"name" binding:"required,min=2,max=50" example:"Alice"`
	Email  string   `json:"email" binding:"required,email,email_advanced"`
	Status *string  `json:"status" binding:"omitempty,oneof=active inactive"`
	Tags   []string `json:"tags" binding:"omitempty,max=3,dive,min=1"`
	IDs    []string `json:"ids" binding:"dive,uuid"`
	File   string   `json:"file" binding:"omitempty,file_ext=jpg png"`
	Born   *string  `json:"born" binding:"omitempty,date"`
	Perms  []Perm   `json:"perms" binding:"dive,required"`
	Kind   kind     `json:"kind"`
	Phone  *string  `json:"phone" binding:"omitzero,phone_advanced"`
	Nick   *string  `json:"nick" binding:"omitempty,min=2"`
	Code   string   `json:"code" binding:"omitempty,min=2"`
	Slug   string   `json:"slug" binding:"required" pattern:"^[a-z_]+$"`
}

type listQuery struct {
	pagination.Request
}

type itemResponse struct {
	UUID      uuid.UUID `json:"uuid"`
	CreatedAt time.Time `json:"created_at"`
	Note      *string   `json:"note,omitempty"`
	Labels    []string  `json:"labels"`
	State     string    `json:"state" enum:"open closed"`
	Perms     []Perm    `json:"perms"`
	Owner     *Perm     `json:"owner"`
	Secret    string    `json:"-"`
}

// Perm is a string type with an enum in the spec; kind is one without.
type (
	Perm string
	kind string
)

var (
	errTaken    = response.New(response.Conflict, "name_taken")
	errNotFound = response.New(response.NotFound, "item_not_found")
	errGone     = response.New(response.NotFound, "api_not_found")
	errTooLong  = response.New(response.UnprocessableEntity, "name_too_long")
)

// obj is a JSON object decoded from the spec.
type obj = map[string]any

// testSpec builds and decodes the spec shared by the Spec tests.
func testSpec(t *testing.T) obj {
	t.Helper()

	raw, err := apidoc.Spec(apidoc.Info{Title: "Test", Version: "v1", Enums: []apidoc.Enum{apidoc.EnumOf[Perm]("a:read", "a:write")}}, []apidoc.Operation{
		{
			Route: apidoc.Route{ID: "createItem", Method: http.MethodPost, Path: "/api/v1/items", Summary: "Create",
				Body: createRequest{}, Status: http.StatusCreated, Response: itemResponse{},
				Headers: []apidoc.Header{{Name: "X-Device-ID", Description: "The device"}},
				Errors:  []*response.AppError{errTaken, errTooLong, errTaken}},
			Tag:      "items",
			Security: apidoc.Security{Bearer: true, Permissions: []string{"items:create"}},
		},
		{
			Route: apidoc.Route{ID: "listItems", Method: http.MethodGet, Path: "/api/v1/items", Summary: "List",
				Query: listQuery{}, Response: itemResponse{}, Paginated: true},
		},
		{
			Route: apidoc.Route{ID: "deleteItem", Method: http.MethodDelete, Path: "/api/internal/items/:uuid", Summary: "Delete",
				Errors: []*response.AppError{errGone, errNotFound}},
			Security: apidoc.Security{APIKeyHeader: "X-Internal-Secret"},
		},
		{
			Route: apidoc.Route{ID: "uploadFile", Method: http.MethodPost, Path: "/api/v1/items/:id/files", Summary: "Upload",
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

// errorVariant is the error envelope the spec gives for codes and reasons; withError adds "error".
func errorVariant(codes, reasons []string, withError bool) obj {
	props := obj{
		"status":   obj{"type": "string", "enum": []string{"error"}},
		"code":     obj{"type": "string", "enum": codes},
		"reason":   obj{"type": "string", "enum": reasons},
		"message":  obj{"type": "string"},
		"api_info": obj{"$ref": "#/components/schemas/APIInfo"},
	}
	required := []string{"status", "code", "reason", "message", "api_info"}

	if withError {
		props["error"] = validationDetail
		required = append(required, "error")
	}

	return obj{"type": "object", "properties": props, "required": required}
}

// validationDetail is the "error" of a validation error.
var validationDetail = obj{"oneOf": []obj{
	{"type": "object", "additionalProperties": obj{"type": "string"}},
	{"type": "string"},
}}

// errorSchema returns the JSON schema of op's response for status.
func errorSchema(t *testing.T, op obj, status string) obj {
	t.Helper()
	return at(t, op, "responses", status, "content", "application/json", "schema")
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
	errComponent := at(t, doc, "components", "schemas", "Error")

	listParams, ok := list["parameters"].([]any)
	if !ok {
		t.Fatalf("list parameters = %T, want a JSON array", list["parameters"])
	}

	if len(listParams) != 5 {
		t.Fatalf("query param count = %d, want 5", len(listParams))
	}

	emailPattern, _ := validation.Pattern("email_advanced")
	phonePattern, _ := validation.Pattern("phone_advanced")

	tests := []struct {
		name string
		got  any
		want any
	}{
		{"body lists required fields", body["required"], []string{"name", "email", "slug"}},
		{"pattern tag", props["slug"], obj{"type": "string", "pattern": "^[a-z_]+$"}},
		{"string with length limits and example", props["name"], obj{"type": "string", "minLength": 2, "maxLength": 50, "example": "Alice"}},
		{"email format with custom validator", props["email"], obj{
			"type": "string", "format": "email", "description": "Validated by: email_advanced", "pattern": emailPattern,
			"x-validation": []obj{{"rule": "email_advanced", "pattern": emailPattern}},
		}},
		{"request pointer is optional, not nullable", props["status"], obj{"type": "string", "enum": []string{"active", "inactive"}}},
		{"array with item limits", props["tags"], obj{"type": "array", "maxItems": 3, "items": obj{"type": "string", "minLength": 1}}},
		{"array of uuids", props["ids"], obj{"type": "array", "items": obj{"type": "string", "format": "uuid"}}},
		{"custom rule keeps its parameter", props["file"], obj{
			"type": "string", "description": "Validated by: file_ext=jpg png",
			"x-validation": []obj{{"rule": "file_ext", "param": "jpg png"}},
		}},
		{"date rule", props["born"], obj{"type": "string", "format": "date"}},
		{"enum type slice in a request", props["perms"], obj{"type": "array", "items": obj{"$ref": "#/components/schemas/Perm"}}},
		{"string type without an enum", props["kind"], obj{"type": "string"}},
		{"omitzero pointer accepts the empty string", props["phone"], obj{"anyOf": []obj{
			{"type": "string", "description": "Validated by: phone_advanced", "pattern": phonePattern,
				"x-validation": []obj{{"rule": "phone_advanced", "pattern": phonePattern}}},
			{"type": "string", "enum": []string{""}},
		}}},
		{"omitempty pointer validates the empty string", props["nick"], obj{"type": "string", "minLength": 2}},
		{"omitempty plain string accepts the empty string", props["code"], obj{"anyOf": []obj{
			{"type": "string", "minLength": 2},
			{"type": "string", "enum": []string{""}},
		}}},
		{"bearer security", create["security"], []map[string][]string{{"bearerAuth": {}}}},
		{"required permissions", create["x-permissions"], []string{"items:create"}},
		{"response data schema", data["properties"], obj{
			"uuid":       obj{"type": "string", "format": "uuid"},
			"created_at": obj{"type": "string", "format": "date-time"},
			"note":       obj{"type": "string", "nullable": true},
			"labels":     obj{"type": "array", "items": obj{"type": "string"}},
			"state":      obj{"type": "string", "enum": []string{"open", "closed"}},
			"perms":      obj{"type": "array", "items": obj{"$ref": "#/components/schemas/Perm"}},
			"owner":      obj{"allOf": []obj{{"$ref": "#/components/schemas/Perm"}}, "nullable": true},
		}},
		{"response requires fields its json tag never omits", data["required"], []string{"uuid", "created_at", "labels", "state", "perms", "owner"}},
		{"success envelope required fields", envelope["required"], []string{"status", "message", "api_info", "data"}},
		{"success envelope status", at(t, envelope, "properties", "status"), obj{"type": "string", "enum": []string{"success"}}},
		{"paginated data required fields",
			at(t, list, "responses", "200", "content", "application/json", "schema", "properties", "data")["required"],
			[]string{"items", "pagination"}},
		{"pagination component required fields", at(t, doc, "components", "schemas", "Pagination")["required"],
			[]string{"page", "page_size", "total_items", "total_pages", "has_next", "has_prev", "next_page", "prev_page"}},
		{"api info component required fields", at(t, doc, "components", "schemas", "APIInfo")["required"], []string{"timestamp"}},
		{"named enum component", at(t, doc, "components", "schemas", "Perm"), obj{"type": "string", "enum": []string{"a:read", "a:write"}}},
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
		{"route without errors documents only its success", slices.Sorted(maps.Keys(at(t, upload, "responses"))), []string{"200"}},
		{"operation ids", []any{create["operationId"], list["operationId"], del["operationId"], upload["operationId"]},
			[]string{"createItem", "listItems", "deleteItem", "uploadFile"}},
		{"header parameter", create["parameters"], []obj{{"name": "X-Device-ID", "in": "header", "required": false,
			"description": "The device", "schema": obj{"type": "string"}}}},
		{"one error kind per status", errorSchema(t, create, "409"), errorVariant([]string{"CONFLICT"}, []string{"name_taken"}, false)},
		{"errors of one status share a response", errorSchema(t, del, "404"),
			errorVariant([]string{"NOT_FOUND"}, []string{"api_not_found", "item_not_found"}, false)},
		{"422 with only validation", errorSchema(t, list, "422"),
			errorVariant([]string{"UNPROCESSABLE_ENTITY"}, []string{"validation_error"}, true)},
		{"422 with validation and other reasons", errorSchema(t, create, "422"), obj{"oneOf": []obj{
			errorVariant([]string{"UNPROCESSABLE_ENTITY"}, []string{"validation_error"}, true),
			errorVariant([]string{"UNPROCESSABLE_ENTITY"}, []string{"name_too_long"}, false),
		}}},
		{"body route response codes", slices.Sorted(maps.Keys(at(t, create, "responses"))), []string{"201", "409", "422"}},
		{"error component required fields", errComponent["required"], []string{"status", "code", "reason", "message", "api_info"}},
		{"error component status", at(t, errComponent, "properties", "status"), obj{"type": "string", "enum": []string{"error"}}},
		{"error component codes", at(t, errComponent, "properties", "code"),
			obj{"type": "string", "enum": []string{"CONFLICT", "NOT_FOUND", "UNPROCESSABLE_ENTITY"}}},
		{"error component reasons", at(t, errComponent, "properties", "reason"),
			obj{"type": "string", "enum": []string{"api_not_found", "item_not_found", "name_taken", "name_too_long", "validation_error"}}},
		{"error component detail", at(t, errComponent, "properties", "error"), validationDetail},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertJSON(t, tt.got, tt.want)
		})
	}
}

func TestSpec_Rejects(t *testing.T) {
	route := apidoc.Route{ID: "getItem", Method: http.MethodGet, Path: "/items", Summary: "Get"}

	tests := []struct {
		name string
		info apidoc.Info
		ops  []apidoc.Operation
		want string
	}{
		{"operation without an ID", apidoc.Info{}, []apidoc.Operation{{Route: apidoc.Route{Method: http.MethodGet, Path: "/items"}}},
			"apidoc: GET /items has no ID"},
		{"two operations sharing an ID", apidoc.Info{}, []apidoc.Operation{{Route: route}, {Route: route}},
			`apidoc: ID "getItem" is used by two operations`},
		{"two enums of one type name", apidoc.Info{Enums: []apidoc.Enum{apidoc.EnumOf[Perm]("a"), apidoc.EnumOf[Perm]("b")}},
			[]apidoc.Operation{{Route: route}}, `apidoc: enum type name "Perm" is already taken`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := apidoc.Spec(tt.info, tt.ops)
			if err == nil || err.Error() != tt.want {
				t.Fatalf("Spec() error = %v, want %q", err, tt.want)
			}
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

func TestSpec_TypesPathParameters(t *testing.T) {
	tests := []struct {
		name string
		path string
		want map[string]any
	}{
		{":family_uuid", "/sessions/:family_uuid", map[string]any{"type": "string", "format": "uuid"}},
		{":id", "/items/:id", map[string]any{"type": "integer"}},
		{":slug", "/pages/:slug", map[string]any{"type": "string"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec, err := apidoc.Spec(apidoc.Info{}, []apidoc.Operation{{Route: apidoc.Route{ID: "op", Method: http.MethodGet, Path: tt.path}}})
			if err != nil {
				t.Fatalf("Spec() error = %v", err)
			}

			var doc struct {
				Paths map[string]map[string]struct {
					Parameters []struct {
						Schema map[string]any `json:"schema"`
					} `json:"parameters"`
				} `json:"paths"`
			}
			if err := json.Unmarshal(spec, &doc); err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}

			for _, op := range doc.Paths {
				if got := op["get"].Parameters; len(got) != 1 || !reflect.DeepEqual(got[0].Schema, tt.want) {
					t.Errorf("path %s parameters = %+v, want one with schema %v", tt.path, got, tt.want)
				}
			}
		})
	}
}
