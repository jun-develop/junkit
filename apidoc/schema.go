package apidoc

import (
	"encoding"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
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

// schemaOf returns the JSON schema of t, naming struct fields by tagKey ("json" or "form").
func schemaOf(t reflect.Type, tagKey string) map[string]any {
	return schemaFor(t, tagKey, map[reflect.Type]bool{})
}

// schemaFor is schemaOf with seen tracking the structs being walked, so recursive types end.
func schemaFor(t reflect.Type, tagKey string, seen map[reflect.Type]bool) map[string]any {
	if t.Kind() == reflect.Pointer {
		s := schemaFor(t.Elem(), tagKey, seen)
		s["nullable"] = true
		return s
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

		return map[string]any{"type": "array", "items": schemaFor(t.Elem(), tagKey, seen)}
	case reflect.Map:
		return map[string]any{"type": "object", "additionalProperties": schemaFor(t.Elem(), tagKey, seen)}
	case reflect.Struct:
		return structSchema(t, tagKey, seen)
	}

	// interface{} and anything else: any value.
	return map[string]any{}
}

// queryParams returns the query parameters of struct v, named by their form tags.
func queryParams(v any) []map[string]any {
	var params []map[string]any
	for _, f := range fields(typeOf(v), "form") {
		s := schemaOf(f.typ, "form")
		delete(s, "nullable")
		required := applyRules(s, f.binding)
		setExample(s, f.example)
		params = append(params, map[string]any{"name": f.name, "in": "query", "required": required, "schema": s})
	}

	return params
}

// field is one struct field as the schema sees it.
type field struct {
	name, binding, example string
	typ                    reflect.Type
}

// fields lists t's exported fields by their tagKey name, flattening embedded structs.
func fields(t reflect.Type, tagKey string) []field {
	var out []field
	for i := range t.NumField() {
		f := t.Field(i)
		name, _, _ := strings.Cut(f.Tag.Get(tagKey), ",")
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

		out = append(out, field{name: name, binding: f.Tag.Get("binding"), example: f.Tag.Get("example"), typ: f.Type})
	}

	return out
}

// applyRules turns a validator binding tag into schema keywords on s and reports whether the field
// is required. Rules after "dive" apply to the items of a slice. Rules OpenAPI cannot express (custom
// validators) are listed in the description.
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
		default:
			other = append(other, rule)
		}
	}

	if len(other) > 0 {
		s["description"] = "Validated by: " + strings.Join(other, ", ")
	}

	return required
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

// structSchema returns the object schema of struct t, or a bare object when t is already being
// walked (a recursive type).
func structSchema(t reflect.Type, tagKey string, seen map[reflect.Type]bool) map[string]any {
	if seen[t] {
		return map[string]any{"type": "object"}
	}

	seen[t] = true
	defer delete(seen, t)

	props := map[string]any{}
	var required []string
	for _, f := range fields(t, tagKey) {
		s := schemaFor(f.typ, tagKey, seen)
		if applyRules(s, f.binding) {
			required = append(required, f.name)
		}

		setExample(s, f.example)
		props[f.name] = s
	}

	s := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		s["required"] = required
	}

	return s
}
