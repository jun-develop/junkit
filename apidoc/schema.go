package apidoc

import (
	"encoding"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/jun-develop/junkit/validation"
)

// Types that get a fixed schema instead of being walked by reflection.
var (
	timeType          = reflect.TypeFor[time.Time]()
	uuidType          = reflect.TypeFor[uuid.UUID]()
	textMarshalerType = reflect.TypeFor[encoding.TextMarshaler]()
)

// typeOf returns v's type with pointers removed.
func typeOf(v any) reflect.Type {
	t := reflect.TypeOf(v)
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	return t
}

// schemaOf returns the JSON schema of t, naming fields by tagKey ("json" or "form"). Response fields are
// required unless omitempty/omitzero; others take required from binding tags. A response pointer is
// nullable; a request pointer only marks the field optional, since null there means "not sent". Slices
// and maps are not nullable: DTOs never return a nil one, and a list that can be null is declared *[]T.
// A type with an enum (see Info.Enums) is a $ref to its component schema.
func schemaOf(t reflect.Type, tagKey string, response bool, enums map[reflect.Type]string) map[string]any {
	w := &walker{tagKey: tagKey, response: response, enums: enums, seen: map[reflect.Type]bool{}}
	return w.schema(t)
}

// walker builds the schema of one type; seen tracks the structs being walked, so recursive types end.
type walker struct {
	tagKey   string
	response bool
	enums    map[reflect.Type]string
	seen     map[reflect.Type]bool
}

// schema returns the JSON schema of t.
func (w *walker) schema(t reflect.Type) map[string]any {
	if t.Kind() == reflect.Pointer {
		s := w.schema(t.Elem())
		if w.response {
			s["nullable"] = true
		}

		return refOnly(s)
	}

	if name, ok := w.enums[t]; ok {
		return map[string]any{"$ref": "#/components/schemas/" + name}
	}

	switch {
	case t == timeType:
		return map[string]any{"type": "string", "format": "date-time"}
	case t == uuidType:
		return map[string]any{"type": "string", "format": "uuid"}
	case t.Implements(textMarshalerType) || reflect.PointerTo(t).Implements(textMarshalerType):
		return map[string]any{"type": "string"}
	}

	switch t.Kind() {
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32:
		return map[string]any{"type": "integer"}
	case reflect.Int64, reflect.Uint64:
		return map[string]any{"type": "integer", "format": "int64"}
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Slice, reflect.Array:
		if t.Elem().Kind() == reflect.Uint8 {
			return map[string]any{"type": "string", "format": "byte"}
		}

		return map[string]any{"type": "array", "items": w.schema(t.Elem())}
	case reflect.Map:
		return map[string]any{"type": "object", "additionalProperties": w.schema(t.Elem())}
	case reflect.Struct:
		return w.object(t)
	}

	// interface{} and anything else: any value.
	return map[string]any{}
}

// queryParams returns the query parameters of struct v, named by their form tags.
func queryParams(v any, enums map[reflect.Type]string) []map[string]any {
	var params []map[string]any
	for _, f := range fields(typeOf(v), "form") {
		s := schemaOf(f.typ, "form", false, enums)
		required := applyRules(s, f.binding)
		applyPattern(s, f.pattern)
		setExample(s, f.example)
		s = refOnly(s)
		params = append(params, map[string]any{"name": f.name, "in": "query", "required": required, "schema": s})
	}

	return params
}

// field is one struct field as the schema sees it.
type field struct {
	name, binding, example, enum, pattern string
	omit                                  bool
	typ                                   reflect.Type
}

// fields lists t's exported fields by their tagKey name, flattening embedded structs.
func fields(t reflect.Type, tagKey string) []field {
	var out []field
	for i := range t.NumField() {
		f := t.Field(i)
		name, opts, _ := strings.Cut(f.Tag.Get(tagKey), ",")
		if name == "-" {
			continue
		}

		if f.Anonymous && name == "" {
			ft := f.Type
			if ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}

			if ft.Kind() == reflect.Struct {
				out = append(out, fields(ft, tagKey)...)
				continue
			}
		}

		if !f.IsExported() {
			continue
		}

		if name == "" {
			name = f.Name
		}

		omit := slices.ContainsFunc(strings.Split(opts, ","), func(o string) bool {
			return o == "omitempty" || o == "omitzero"
		})

		out = append(out, field{
			name: name, binding: f.Tag.Get("binding"), example: f.Tag.Get("example"),
			enum: f.Tag.Get("enum"), pattern: f.Tag.Get("pattern"), omit: omit, typ: f.Type,
		})
	}

	return out
}

// applyRules converts binding tags into schema constraints and reports whether the field is required.
func applyRules(s map[string]any, binding string) bool {
	if binding == "" {
		return false
	}

	before, after, dive := strings.Cut(","+binding, ",dive")
	if dive {
		if items, ok := s["items"].(map[string]any); ok {
			applyRules(items, strings.TrimPrefix(after, ","))
		}
	}

	required := false
	var other []string
	for rule := range strings.SplitSeq(before, ",") {
		name, param, _ := strings.Cut(rule, "=")
		switch name {
		case "", "omitempty", "omitzero", "omitnil":
		case "required":
			required = true
		case "min", "gte":
			setBound(s, param, "minLength", "minimum", "minItems")
		case "max", "lte":
			setBound(s, param, "maxLength", "maximum", "maxItems")
		case "len":
			setBound(s, param, "minLength", "minimum", "minItems")
			setBound(s, param, "maxLength", "maximum", "maxItems")
		case "oneof":
			var enum []any
			for v := range strings.FieldsSeq(param) {
				enum = append(enum, typed(s, v))
			}
			s["enum"] = enum
		case "email":
			s["format"] = "email"
		case "uuid", "uuid4":
			s["format"] = "uuid"
		case "url", "uri", "http_url":
			s["format"] = "uri"
		case "date":
			s["format"] = "date"
		default:
			other = append(other, rule)
		}
	}

	if len(other) > 0 {
		s["description"] = "Validated by: " + strings.Join(other, ", ")
		s["x-validation"] = customRules(s, other)
	}

	return required
}

// customRules describes the rules no standard keyword covers as x-validation entries, so a client can
// mirror them, and sets s's pattern from a rule that has one.
func customRules(s map[string]any, rules []string) []map[string]string {
	out := make([]map[string]string, 0, len(rules))
	for _, rule := range rules {
		name, param, _ := strings.Cut(rule, "=")
		entry := map[string]string{"rule": name}
		if param != "" {
			entry["param"] = param
		}

		if p, ok := validation.Pattern(name); ok && s["type"] == "string" {
			s["pattern"] = p
			entry["pattern"] = p
		}

		out = append(out, entry)
	}

	return out
}

// applyEnum limits s, or its items when s is an array, to the values of an enum tag.
func applyEnum(s map[string]any, enum string) {
	if enum == "" {
		return
	}

	target := s
	if items, ok := s["items"].(map[string]any); ok {
		target = items
	}

	var values []any
	for v := range strings.FieldsSeq(enum) {
		values = append(values, typed(target, v))
	}

	target["enum"] = values
}

// applyPattern sets s's pattern, or its items' when s is an array, from a pattern tag: the ECMAScript
// regular expression a check outside the binding tags (e.g. in a service) holds the value to.
func applyPattern(s map[string]any, pattern string) {
	if pattern == "" {
		return
	}

	if items, ok := s["items"].(map[string]any); ok {
		s = items
	}

	s["pattern"] = pattern
}

// refOnly moves the keywords next to a $ref into an allOf, since OpenAPI 3.0 ignores a $ref's
// siblings; an array gets the same for its items.
func refOnly(s map[string]any) map[string]any {
	if items, ok := s["items"].(map[string]any); ok {
		s["items"] = refOnly(items)
	}

	r, ok := s["$ref"]
	if !ok || len(s) == 1 {
		return s
	}

	out := map[string]any{"allOf": []any{map[string]any{"$ref": r}}}
	for k, v := range s {
		if k != "$ref" {
			out[k] = v
		}
	}

	return out
}

// setBound sets the length, value or item-count keyword that fits s's type.
func setBound(s map[string]any, param, strKey, numKey, arrKey string) {
	n, err := strconv.ParseFloat(param, 64)
	if err != nil {
		return
	}

	switch s["type"] {
	case "string":
		s[strKey] = n
	case "integer", "number":
		s[numKey] = n
	case "array":
		s[arrKey] = n
	}
}

// setExample sets s's example from an example tag, typed to match s.
func setExample(s map[string]any, example string) {
	if example != "" {
		s["example"] = typed(s, example)
	}
}

// typed converts v to the JSON type of s, so an integer example is not shown quoted.
func typed(s map[string]any, v string) any {
	switch s["type"] {
	case "integer":
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	case "number":
		if n, err := strconv.ParseFloat(v, 64); err == nil {
			return n
		}
	case "boolean":
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}

	return v
}

// skipsEmpty reports whether the validator skips f's rules for an empty string, so the server accepts
// "" whatever the rules say: omitzero skips it even behind a pointer, omitempty only on a plain field
// (on a pointer it skips nil alone).
func skipsEmpty(f field) bool {
	rules, _, _ := strings.Cut(f.binding, ",dive")
	for rule := range strings.SplitSeq(rules, ",") {
		if rule == "omitzero" || rule == "omitempty" && f.typ.Kind() != reflect.Pointer {
			return true
		}
	}

	return false
}

// rejectsEmpty reports whether string schema s, as built, would turn down "".
func rejectsEmpty(s map[string]any) bool {
	if s["type"] != "string" {
		return false
	}

	minLen, _ := s["minLength"].(float64)

	return s["pattern"] != nil || s["enum"] != nil || s["format"] != nil || minLen > 0
}

// object builds an object schema for struct t.
func (w *walker) object(t reflect.Type) map[string]any {
	if w.seen[t] {
		return map[string]any{"type": "object"}
	}

	w.seen[t] = true
	defer delete(w.seen, t)

	props := map[string]any{}
	var required []string
	for _, f := range fields(t, w.tagKey) {
		s := w.schema(f.typ)
		req := applyRules(s, f.binding)
		applyEnum(s, f.enum)
		applyPattern(s, f.pattern)
		if w.response {
			req = !f.omit
		}

		if req {
			required = append(required, f.name)
		}

		setExample(s, f.example)
		s = refOnly(s)
		if !w.response && skipsEmpty(f) && rejectsEmpty(s) {
			s = map[string]any{"anyOf": []any{s, map[string]any{"type": "string", "enum": []string{""}}}}
		}

		props[f.name] = s
	}

	s := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		s["required"] = required
	}

	return s
}
