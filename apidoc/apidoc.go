// Package apidoc generates OpenAPI 3.0 specs from Go structs and tags, served via Scalar.
package apidoc

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/jun-develop/junkit/response"
	"github.com/jun-develop/junkit/validation"
)

// Route documents an API endpoint; Method and Path match router definitions (e.g. group.POST).
//
// Usage:
//
//	apidoc.Route{
//	    ID:       "createUser",
//	    Method:   http.MethodPost,
//	    Path:     "/users",
//	    Summary:  "Create a user",
//	    Body:     CreateUserRequest{},
//	    Status:   http.StatusCreated,
//	    Response: UserResponse{},
//	    Errors:   []*response.AppError{domain.ErrEmailAlreadyExists},
//	}
type Route struct {
	// ID is the operationId, unique in the document; client generators name their functions after it.
	ID          string
	Method      string
	Path        string
	Summary     string
	Description string

	// Query is the struct bound from the query string (form tags).
	Query any
	// Body is the struct bound from a JSON body (json tags).
	Body any
	// Files lists multipart file fields; the body is then multipart/form-data.
	Files []string
	// Headers are the request headers the route reads, besides the ones its Security covers.
	Headers []Header

	// Status is the success status; 0 means 200.
	Status int
	// Response is the value under "data"; nil when the response carries only a message.
	Response any
	// Paginated marks Response as one item of a paginated list.
	Paginated bool

	// Errors are every error the route can answer with. A route with a Query or Body also gets
	// validation.ErrValidation, which the bind helpers answer. Each status becomes one response whose
	// code and reason list exactly the errors given for it.
	Errors []*response.AppError
}

// Header documents a request header.
type Header struct {
	Name        string
	Description string
	Required    bool
}

// Security says how an operation is protected.
type Security struct {
	// Bearer means the operation needs "Authorization: Bearer <token>".
	Bearer bool
	// APIKeyHeader names a header that must hold a shared secret.
	APIKeyHeader string
	// Permissions are the permission codes the caller must hold.
	Permissions []string
}

// Operation is a Route placed at its full path, with its tag and security.
type Operation struct {
	Route
	Tag      string
	Security Security
}

// Info names the API in the document and lists the enums of its types.
type Info struct {
	Title   string
	Version string
	// Enums give string types the values they can hold. Each becomes a component schema named after
	// its type, and every field of that type refers to it. For value sets known only at run time; a
	// fixed set fits an enum tag on the field instead.
	Enums []Enum
}

// Enum is the set of values a string type can hold, built with EnumOf.
type Enum struct {
	typ    reflect.Type
	values []string
}

// EnumOf returns the Enum of type T: a field of type T (or []T, *T) may only hold values.
//
// Usage:
//
//	type PermissionCode string
//	apidoc.Info{Enums: []apidoc.Enum{apidoc.EnumOf[PermissionCode](codes...)}}
func EnumOf[T ~string](values ...string) Enum {
	return Enum{typ: reflect.TypeFor[T](), values: values}
}

// Spec returns the OpenAPI 3.0 JSON document for ops; full paths use gin's syntax (/users/:uuid). It
// fails when an operation has no ID, two share one, or an enum type's name is already taken.
func Spec(info Info, ops []Operation) ([]byte, error) {
	enumNames := map[reflect.Type]string{}
	enumSchemas := map[string]any{}
	for _, e := range info.Enums {
		name := e.typ.Name()
		if _, ok := enumSchemas[name]; ok || slices.Contains([]string{"APIInfo", "Pagination", "Error"}, name) {
			return nil, fmt.Errorf("apidoc: enum type name %q is already taken", name)
		}

		enumNames[e.typ] = name
		enumSchemas[name] = map[string]any{"type": "string", "enum": e.values}
	}

	sort.SliceStable(ops, func(i, j int) bool {
		if ops[i].Path != ops[j].Path {
			return ops[i].Path < ops[j].Path
		}

		return methodOrder(ops[i].Method) < methodOrder(ops[j].Method)
	})

	paths := map[string]map[string]any{}
	schemes := map[string]any{}
	used := &errorSet{}
	ids := map[string]bool{}

	var tags []map[string]string
	seenTag := map[string]bool{}

	for _, op := range ops {
		if op.ID == "" {
			return nil, fmt.Errorf("apidoc: %s %s has no ID", op.Method, op.Path)
		}

		if ids[op.ID] {
			return nil, fmt.Errorf("apidoc: ID %q is used by two operations", op.ID)
		}

		ids[op.ID] = true

		path, params := openAPIPath(op.Path)
		if paths[path] == nil {
			paths[path] = map[string]any{}
		}

		paths[path][strings.ToLower(op.Method)] = operation(op, params, schemes, used, enumNames)

		if op.Tag != "" && !seenTag[op.Tag] {
			seenTag[op.Tag] = true
			tags = append(tags, map[string]string{"name": op.Tag})
		}
	}

	schemas := map[string]any{
		"APIInfo":    apiInfoSchema,
		"Pagination": paginationSchema,
		"Error":      errorComponent(used),
	}

	maps.Copy(schemas, enumSchemas)

	doc := map[string]any{
		"openapi": "3.0.3",
		"info":    map[string]string{"title": info.Title, "version": info.Version},
		"paths":   paths,
		"components": map[string]any{
			"securitySchemes": schemes,
			"schemas":         schemas,
		},
	}

	if len(tags) > 0 {
		doc["tags"] = tags
	}

	return json.MarshalIndent(doc, "", "  ")
}

// operation builds one OpenAPI operation object and records the security schemes and errors it uses.
func operation(op Operation, pathParams []map[string]any, schemes map[string]any, used *errorSet, enums map[reflect.Type]string) map[string]any {
	o := map[string]any{"operationId": op.ID, "summary": op.Summary}
	if op.Tag != "" {
		o["tags"] = []string{op.Tag}
	}

	desc := op.Description
	if len(op.Security.Permissions) > 0 {
		desc = strings.TrimSpace("Requires permission: `" + strings.Join(op.Security.Permissions, "`, `") + "`.\n\n" + desc)
		o["x-permissions"] = op.Security.Permissions
	}

	if desc != "" {
		o["description"] = desc
	}

	params := pathParams
	for _, h := range op.Headers {
		p := map[string]any{"name": h.Name, "in": "header", "required": h.Required, "schema": map[string]string{"type": "string"}}
		if h.Description != "" {
			p["description"] = h.Description
		}

		params = append(params, p)
	}

	if op.Query != nil {
		params = append(params, queryParams(op.Query, enums)...)
	}

	if len(params) > 0 {
		o["parameters"] = params
	}

	if body := requestBody(op.Route, enums); body != nil {
		o["requestBody"] = body
	}

	var security []map[string][]string
	if op.Security.Bearer {
		schemes["bearerAuth"] = map[string]string{"type": "http", "scheme": "bearer"}
		security = append(security, map[string][]string{"bearerAuth": {}})
	}

	if h := op.Security.APIKeyHeader; h != "" {
		schemes[h] = map[string]string{"type": "apiKey", "in": "header", "name": h}
		security = append(security, map[string][]string{h: {}})
	}

	if security != nil {
		o["security"] = security
	}

	o["responses"] = responses(op, used, enums)

	return o
}

// requestBody returns the JSON or multipart body of r, or nil when it has none.
func requestBody(r Route, enums map[reflect.Type]string) map[string]any {
	switch {
	case len(r.Files) > 0:
		props := map[string]any{}
		for _, f := range r.Files {
			props[f] = map[string]string{"type": "string", "format": "binary"}
		}

		schema := map[string]any{"type": "object", "properties": props, "required": r.Files}

		return map[string]any{"required": true, "content": map[string]any{"multipart/form-data": map[string]any{"schema": schema}}}
	case r.Body != nil:
		return map[string]any{"required": true, "content": jsonContent(schemaOf(typeOf(r.Body), "json", false, enums))}
	}

	return nil
}

// responses returns the success response in the app's envelope plus one response per error status.
func responses(op Operation, used *errorSet, enums map[reflect.Type]string) map[string]any {
	status := op.Status
	if status == 0 {
		status = http.StatusOK
	}

	envelope := map[string]any{
		"status":   map[string]any{"type": "string", "enum": []string{"success"}},
		"message":  map[string]string{"type": "string"},
		"api_info": ref("APIInfo"),
	}
	required := []string{"status", "message", "api_info"}

	if op.Response != nil {
		data := schemaOf(typeOf(op.Response), "json", true, enums)
		if op.Paginated {
			data = map[string]any{"type": "object", "properties": map[string]any{
				"items":      map[string]any{"type": "array", "items": data},
				"pagination": ref("Pagination"),
			}, "required": []string{"items", "pagination"}}
		}

		envelope["data"] = data
		required = append(required, "data")
	}

	out := map[string]any{
		strconv.Itoa(status): map[string]any{
			"description": http.StatusText(status),
			"content":     jsonContent(map[string]any{"type": "object", "properties": envelope, "required": required}),
		},
	}

	errs := op.Errors
	if op.Body != nil || op.Query != nil {
		errs = append([]*response.AppError{validation.ErrValidation}, errs...)
	}

	byStatus := map[int]*errorSet{}
	for _, e := range errs {
		code := e.Code.HTTPStatus()
		if byStatus[code] == nil {
			byStatus[code] = &errorSet{}
		}

		byStatus[code].add(e)
		used.add(e)
	}

	for code, set := range byStatus {
		out[strconv.Itoa(code)] = map[string]any{"description": http.StatusText(code), "content": jsonContent(set.schema())}
	}

	return out
}

// errorSet collects errors in the order first added, without repeats.
type errorSet struct {
	codes, reasons []string
	validation     bool
}

// add records e.
func (s *errorSet) add(e *response.AppError) {
	if e.Message == validation.ErrValidation.Message {
		s.validation = true
	}

	if !slices.Contains(s.codes, string(e.Code)) {
		s.codes = append(s.codes, string(e.Code))
	}

	if !slices.Contains(s.reasons, e.Message) {
		s.reasons = append(s.reasons, e.Message)
	}
}

// schema returns the error envelope for s's errors. A validation error is a variant of its own, the
// only one that carries "error", so a client can tell them apart by reason.
func (s *errorSet) schema() map[string]any {
	other := slices.DeleteFunc(slices.Clone(s.reasons), func(r string) bool { return r == validation.ErrValidation.Message })

	switch {
	case !s.validation:
		return errorVariant(s.codes, other, false)
	case len(other) == 0:
		return errorVariant([]string{string(validation.ErrValidation.Code)}, []string{validation.ErrValidation.Message}, true)
	}

	return map[string]any{"oneOf": []any{
		errorVariant([]string{string(validation.ErrValidation.Code)}, []string{validation.ErrValidation.Message}, true),
		errorVariant(s.codes, other, false),
	}}
}

// errorVariant is the error envelope with code and reason limited to the given values; withError adds
// the required "error" a validation error carries.
func errorVariant(codes, reasons []string, withError bool) map[string]any {
	props := map[string]any{
		"status":   map[string]any{"type": "string", "enum": []string{"error"}},
		"code":     stringEnum(codes),
		"reason":   stringEnum(reasons),
		"message":  map[string]string{"type": "string"},
		"api_info": ref("APIInfo"),
	}
	required := []string{"status", "code", "reason", "message", "api_info"}

	if withError {
		props["error"] = validationDetail
		required = append(required, "error")
	}

	return map[string]any{"type": "object", "properties": props, "required": required}
}

// stringEnum is a string schema limited to values, or any string when there are none.
func stringEnum(values []string) map[string]any {
	if len(values) == 0 {
		return map[string]any{"type": "string"}
	}

	return map[string]any{"type": "string", "enum": values}
}

// errorComponent is the Error component: any error the document lists, so a client has one type for
// every error response.
func errorComponent(used *errorSet) map[string]any {
	s := errorVariant(slices.Sorted(slices.Values(used.codes)), slices.Sorted(slices.Values(used.reasons)), false)
	s["properties"].(map[string]any)["error"] = validationDetail
	s["description"] = "\"error\" is present only when reason is " + validation.ErrValidation.Message + "."

	return s
}

// validationDetail is the "error" of a validation error: field -> message, or one message when the
// body is not valid JSON at all.
var validationDetail = map[string]any{"oneOf": []any{
	map[string]any{"type": "object", "additionalProperties": map[string]string{"type": "string"}},
	map[string]string{"type": "string"},
}}

var (
	// apiInfoSchema is the api_info object every response carries.
	apiInfoSchema = map[string]any{"type": "object", "properties": map[string]any{
		"trace_id":  map[string]string{"type": "string"},
		"timestamp": map[string]string{"type": "string", "format": "date-time"},
		"latency":   map[string]string{"type": "string"},
		"version":   map[string]string{"type": "string"},
		"env":       map[string]string{"type": "string"},
	}, "required": []string{"timestamp"}} // the other fields are omitempty

	// paginationSchema is the pagination object of a paginated list.
	paginationSchema = map[string]any{"type": "object", "properties": map[string]any{
		"page":        map[string]string{"type": "integer"},
		"page_size":   map[string]string{"type": "integer"},
		"total_items": map[string]string{"type": "integer"},
		"total_pages": map[string]string{"type": "integer"},
		"has_next":    map[string]string{"type": "boolean"},
		"has_prev":    map[string]string{"type": "boolean"},
		"next_page":   map[string]any{"type": "integer", "nullable": true},
		"prev_page":   map[string]any{"type": "integer", "nullable": true},
	}, "required": []string{"page", "page_size", "total_items", "total_pages", "has_next", "has_prev", "next_page", "prev_page"}}
)

// ref returns a $ref to the named component schema.
func ref(name string) map[string]string {
	return map[string]string{"$ref": "#/components/schemas/" + name}
}

// ginParam matches a gin path parameter (:name or *name).
var ginParam = regexp.MustCompile(`[:*]([A-Za-z0-9_]+)`)

// openAPIPath converts Gin path format (/users/:id) to OpenAPI format (/users/{id}) and extracts parameters;
// :uuid and :<name>_uuid are uuid strings, :id an integer.
func openAPIPath(path string) (string, []map[string]any) {
	var params []map[string]any
	for _, m := range ginParam.FindAllStringSubmatch(path, -1) {
		schema := map[string]string{"type": "string"}
		switch {
		case m[1] == "uuid" || strings.HasSuffix(m[1], "_uuid"):
			schema["format"] = "uuid"
		case m[1] == "id":
			schema["type"] = "integer"
		}

		params = append(params, map[string]any{"name": m[1], "in": "path", "required": true, "schema": schema})
	}

	return ginParam.ReplaceAllString(path, "{$1}"), params
}

// jsonContent wraps schema as an application/json content map.
func jsonContent(schema any) map[string]any {
	return map[string]any{"application/json": map[string]any{"schema": schema}}
}

// methodOrder ranks HTTP methods so operations of a path list in a stable, familiar order.
func methodOrder(m string) int {
	for i, x := range []string{"GET", "POST", "PUT", "PATCH", "DELETE"} {
		if m == x {
			return i
		}
	}

	return 9
}

// Register serves the Scalar API reference at path and the document at path + "/openapi.json".
//
// Usage:
//
//	apidoc.Register(router, "/docs", "My API", spec)
func Register(r gin.IRoutes, path, title string, spec []byte) {
	page := strings.NewReplacer("{{title}}", title, "{{spec}}", path+"/openapi.json").Replace(uiPage)

	r.GET(path, func(c *gin.Context) {
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(page))
	})
	r.GET(path+"/openapi.json", func(c *gin.Context) {
		c.Data(http.StatusOK, "application/json", spec)
	})
}

// uiPage is the Scalar HTML page; Register fills in {{title}} and {{spec}}.
const uiPage = `<!doctype html>
<html>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{title}}</title>
</head>
<body>
<div id="app"></div>
<script src="https://cdn.jsdelivr.net/npm/@scalar/api-reference@1.76.4"></script>
<script>Scalar.createApiReference("#app", {url: "{{spec}}", persistAuth: true, hideClientButton: true});</script>
</body>
</html>
`
