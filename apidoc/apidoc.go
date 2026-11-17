// Package apidoc builds an OpenAPI 3.0 document from the request and response structs an app
// already has: field names come from their json/form tags, rules from binding tags, samples from
// example tags. Register serves it with Scalar.
package apidoc

import (
	"encoding/json"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// Route documents one endpoint. Method and Path are relative to the group a feature registers its
// routes on, exactly as passed to group.GET/POST/...
//
// Usage:
//
//	apidoc.Route{Method: http.MethodPost, Path: "/users", Summary: "Create a user",
//	    Body: CreateUserRequest{}, Status: http.StatusCreated, Response: UserResponse{}}
type Route struct {
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

	// Status is the success status; 0 means 200.
	Status int
	// Response is the value under "data"; nil when the response carries only a message.
	Response any
	// Paginated marks Response as one item of a paginated list.
	Paginated bool
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

// Info names the API in the document.
type Info struct {
	Title   string
	Version string
}

// Spec returns the OpenAPI 3.0 JSON document for ops; full paths use gin's syntax (/users/:uuid).
func Spec(info Info, ops []Operation) ([]byte, error) {
	sort.SliceStable(ops, func(i, j int) bool {
		if ops[i].Path != ops[j].Path {
			return ops[i].Path < ops[j].Path
		}

		return methodOrder(ops[i].Method) < methodOrder(ops[j].Method)
	})

	paths := map[string]map[string]any{}
	schemes := map[string]any{}

	var tags []map[string]string
	seenTag := map[string]bool{}

	for _, op := range ops {
		path, params := openAPIPath(op.Path)
		if paths[path] == nil {
			paths[path] = map[string]any{}
		}

		paths[path][strings.ToLower(op.Method)] = operation(op, params, schemes)

		if op.Tag != "" && !seenTag[op.Tag] {
			seenTag[op.Tag] = true
			tags = append(tags, map[string]string{"name": op.Tag})
		}
	}

	doc := map[string]any{
		"openapi": "3.0.3",
		"info":    map[string]string{"title": info.Title, "version": info.Version},
		"paths":   paths,
		"components": map[string]any{
			"securitySchemes": schemes,
			"schemas": map[string]any{
				"APIInfo":    apiInfoSchema,
				"Pagination": paginationSchema,
				"Error":      errorSchema,
			},
		},
	}

	if len(tags) > 0 {
		doc["tags"] = tags
	}

	return json.MarshalIndent(doc, "", "  ")
}

// operation builds one OpenAPI operation object and records the security schemes it uses.
func operation(op Operation, pathParams []map[string]any, schemes map[string]any) map[string]any {
	o := map[string]any{"summary": op.Summary}
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
	if op.Query != nil {
		params = append(params, queryParams(op.Query)...)
	}

	if len(params) > 0 {
		o["parameters"] = params
	}

	if body := requestBody(op.Route); body != nil {
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

	o["responses"] = responses(op, len(pathParams) > 0)

	return o
}

// requestBody returns the JSON or multipart body of r, or nil when it has none.
func requestBody(r Route) map[string]any {
	switch {
	case len(r.Files) > 0:
		props := map[string]any{}
		for _, f := range r.Files {
			props[f] = map[string]string{"type": "string", "format": "binary"}
		}

		schema := map[string]any{"type": "object", "properties": props, "required": r.Files}

		return map[string]any{"required": true, "content": map[string]any{"multipart/form-data": map[string]any{"schema": schema}}}
	case r.Body != nil:
		return map[string]any{"required": true, "content": jsonContent(schemaOf(typeOf(r.Body), "json"))}
	}

	return nil
}

// responses returns the success response in the app's envelope plus the errors the route can answer.
func responses(op Operation, hasPathParams bool) map[string]any {
	status := op.Status
	if status == 0 {
		status = http.StatusOK
	}

	envelope := map[string]any{
		"status":   map[string]any{"type": "string", "example": "success"},
		"message":  map[string]string{"type": "string"},
		"api_info": ref("APIInfo"),
	}

	if op.Response != nil {
		data := schemaOf(typeOf(op.Response), "json")
		if op.Paginated {
			data = map[string]any{"type": "object", "properties": map[string]any{
				"items":      map[string]any{"type": "array", "items": data},
				"pagination": ref("Pagination"),
			}}
		}

		envelope["data"] = data
	}

	out := map[string]any{
		strconv.Itoa(status): map[string]any{
			"description": http.StatusText(status),
			"content":     jsonContent(map[string]any{"type": "object", "properties": envelope}),
		},
	}

	addError := func(code int) {
		out[strconv.Itoa(code)] = map[string]any{"description": http.StatusText(code), "content": jsonContent(ref("Error"))}
	}

	if op.Body != nil || len(op.Files) > 0 || hasPathParams {
		addError(http.StatusBadRequest)
	}

	if op.Security.Bearer || op.Security.APIKeyHeader != "" {
		addError(http.StatusUnauthorized)
	}

	if op.Security.Bearer {
		addError(http.StatusForbidden)
	}

	if op.Body != nil || op.Query != nil {
		addError(http.StatusUnprocessableEntity)
	}

	return out
}

var (
	// apiInfoSchema is the api_info object every response carries.
	apiInfoSchema = map[string]any{"type": "object", "properties": map[string]any{
		"trace_id":  map[string]string{"type": "string"},
		"timestamp": map[string]string{"type": "string", "format": "date-time"},
		"latency":   map[string]string{"type": "string"},
		"version":   map[string]string{"type": "string"},
		"env":       map[string]string{"type": "string"},
	}}

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
	}}

	// errorSchema is the envelope of every error; "error" holds field -> message on a validation error.
	errorSchema = map[string]any{"type": "object", "properties": map[string]any{
		"status":   map[string]any{"type": "string", "example": "error"},
		"code":     map[string]string{"type": "string"},
		"message":  map[string]string{"type": "string"},
		"error":    map[string]any{"type": "object", "additionalProperties": map[string]string{"type": "string"}},
		"api_info": ref("APIInfo"),
	}}
)

// ref returns a $ref to the named component schema.
func ref(name string) map[string]string {
	return map[string]string{"$ref": "#/components/schemas/" + name}
}

// ginParam matches a gin path parameter (:name or *name).
var ginParam = regexp.MustCompile(`[:*]([A-Za-z0-9_]+)`)

// openAPIPath turns /users/:uuid into /users/{uuid} and returns its path parameters. A parameter
// named uuid or id is typed as junkit's RoutePatterns enforce it.
func openAPIPath(path string) (string, []map[string]any) {
	var params []map[string]any
	for _, m := range ginParam.FindAllStringSubmatch(path, -1) {
		schema := map[string]string{"type": "string"}
		switch m[1] {
		case "uuid":
			schema["format"] = "uuid"
		case "id":
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
