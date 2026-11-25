// Package composiotest checks values against the part of Composio's v3.1
// OpenAPI spec that package composio calls, and builds responses from it, so
// tests and fakes cannot drift from what Composio returns.
//
// openapi-v3.1.json is a snapshot shared with scripts/fake-composio.test.mjs.
// Refresh it with `node scripts/composio-spec.mjs update`; a new endpoint goes
// into OPERATIONS in that script first.
package composiotest

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
)

//go:embed openapi-v3.1.json
var snapshot []byte

const apiPrefix = "/api/v3.1"

type document struct {
	Paths      map[string]map[string]operation `json:"paths"`
	Components struct {
		Schemas map[string]any `json:"schemas"`
	} `json:"components"`
}

type operation struct {
	Responses map[string]struct {
		Content map[string]struct {
			Schema any `json:"schema"`
		} `json:"content"`
	} `json:"responses"`
}

var spec = sync.OnceValue(func() document {
	var doc document
	if err := json.Unmarshal(snapshot, &doc); err != nil {
		panic(fmt.Sprintf("composiotest: openapi-v3.1.json: %v", err))
	}
	return doc
})

func lookup(op string) (operation, error) {
	method, path, ok := strings.Cut(op, " ")
	if !ok {
		return operation{}, fmt.Errorf("composiotest: operation %q is not \"METHOD /path\"", op)
	}
	o, ok := spec().Paths[apiPrefix+path][strings.ToLower(method)]
	if !ok {
		return operation{}, fmt.Errorf("composiotest: %s is not in the snapshot; add it to OPERATIONS in scripts/composio-spec.mjs", op)
	}
	return o, nil
}

func responseSchema(op string, status int) (any, error) {
	o, err := lookup(op)
	if err != nil {
		return nil, err
	}
	schema := o.Responses[strconv.Itoa(status)].Content["application/json"].Schema
	if schema == nil {
		return nil, fmt.Errorf("composiotest: the spec declares no %d response for %s", status, op)
	}
	return schema, nil
}

// Operations lists the operations in the snapshot, sorted.
func Operations() []string {
	var ops []string
	for path, methods := range spec().Paths {
		for method := range methods {
			ops = append(ops, strings.ToUpper(method)+" "+strings.TrimPrefix(path, apiPrefix))
		}
	}
	slices.Sort(ops)
	return ops
}

// SuccessStatus returns the 2xx status op answers with.
func SuccessStatus(op string) (int, error) {
	o, err := lookup(op)
	if err != nil {
		return 0, err
	}
	for status := range o.Responses {
		if strings.HasPrefix(status, "2") {
			return strconv.Atoi(status)
		}
	}
	return 0, fmt.Errorf("composiotest: %s has no 2xx response", op)
}

// Check reports where body departs from the schema of op's response with the
// given status: a field the spec does not define, a missing required field, a
// wrong type, or a value outside an enum. op is "METHOD /path" with the path
// as the spec names it under /api/v3.1, e.g. "GET /toolkits/{slug}". body is
// JSON bytes or a value that marshals to JSON.
//
// An object schema with properties and no additionalProperties is closed:
// Composio's spec spells out an open object as additionalProperties:
// {nullable: true}. oneOf is checked like anyOf.
func Check(op string, status int, body any) error {
	return check(op, status, body, false)
}

// CheckFields is Check for a Go value an SDK fake returns from op: every field
// it sets must be one the spec defines, with the spec's type. Required fields
// are not enforced and zero values are skipped, since an unset field marshals
// to one.
func CheckFields(op string, body any) error {
	status, err := SuccessStatus(op)
	if err != nil {
		return err
	}
	return check(op, status, body, true)
}

func check(op string, status int, body any, fields bool) error {
	schema, err := responseSchema(op, status)
	if err != nil {
		return err
	}
	value, err := toJSONValue(body)
	if err != nil {
		return fmt.Errorf("composiotest: %s %d: %w", op, status, err)
	}
	c := checker{schemas: spec().Components.Schemas, fields: fields}
	var problems []string
	c.visit(schema, value, "$", &problems)
	if len(problems) > 0 {
		return fmt.Errorf("%s %d does not match the Composio spec:\n  %s", op, status, strings.Join(problems, "\n  "))
	}
	return nil
}

func toJSONValue(body any) (any, error) {
	raw, ok := body.([]byte)
	if r, isRaw := body.(json.RawMessage); isRaw {
		raw, ok = r, true
	}
	if !ok {
		var err error
		if raw, err = json.Marshal(body); err != nil {
			return nil, err
		}
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	return value, nil
}

type checker struct {
	schemas map[string]any
	fields  bool
}

func (c checker) resolve(schema any) map[string]any {
	m, _ := schema.(map[string]any)
	for m != nil {
		ref, ok := m["$ref"].(string)
		if !ok {
			break
		}
		m, _ = c.schemas[strings.TrimPrefix(ref, "#/components/schemas/")].(map[string]any)
	}
	return m
}

func typeOf(v any) string {
	switch v := v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case float64:
		if v == math.Trunc(v) {
			return "integer"
		}
		return "number"
	case string:
		return "string"
	case []any:
		return "array"
	default:
		return "object"
	}
}

func isZero(v any) bool {
	switch v := v.(type) {
	case nil:
		return true
	case bool:
		return !v
	case float64:
		return v == 0
	case string:
		return v == ""
	case []any:
		return len(v) == 0
	case map[string]any:
		return len(v) == 0
	}
	return false
}

func (c checker) visit(rawSchema, v any, at string, out *[]string) {
	s := c.resolve(rawSchema)
	if s == nil {
		return
	}
	_, hasType := s["type"]
	_, hasProps := s["properties"]
	_, hasItems := s["items"]
	_, hasEnum := s["enum"]
	if v == nil {
		if nullable, _ := s["nullable"].(bool); !nullable && (hasType || hasProps || hasItems || hasEnum) {
			*out = append(*out, at+": is null, the spec does not allow null")
		}
		return
	}
	allOf, _ := s["allOf"].([]any)
	for _, branch := range allOf {
		c.visit(branch, v, at, out)
	}
	alternatives, _ := s["anyOf"].([]any)
	if alternatives == nil {
		alternatives, _ = s["oneOf"].([]any)
	}
	if alternatives != nil {
		var closest []string
		for i, branch := range alternatives {
			var branchProblems []string
			c.visit(branch, v, at, &branchProblems)
			if len(branchProblems) == 0 {
				closest = nil
				break
			}
			if i == 0 || len(branchProblems) < len(closest) {
				closest = branchProblems
			}
		}
		for _, p := range closest {
			*out = append(*out, fmt.Sprintf("%s (closest of %d alternatives)", p, len(alternatives)))
		}
		if !hasType && !hasProps {
			return
		}
	}
	want, _ := s["type"].(string)
	if want == "" {
		if hasProps {
			want = "object"
		} else if hasItems {
			want = "array"
		}
	}
	actual := typeOf(v)
	if want != "" && actual != want && (want != "number" || actual != "integer") {
		*out = append(*out, fmt.Sprintf("%s: is %s, the spec says %s", at, actual, want))
		return
	}
	if enum, ok := s["enum"].([]any); ok && !slices.ContainsFunc(enum, func(e any) bool { return reflect.DeepEqual(e, v) }) {
		raw, _ := json.Marshal(v)
		allowed, _ := json.Marshal(enum)
		*out = append(*out, fmt.Sprintf("%s: %s is not one of %s", at, raw, allowed))
	}
	if items, ok := v.([]any); ok && hasItems {
		for i, item := range items {
			c.visit(s["items"], item, fmt.Sprintf("%s[%d]", at, i), out)
		}
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return
	}
	properties, _ := s["properties"].(map[string]any)
	if !c.fields {
		required, _ := s["required"].([]any)
		for _, name := range required {
			if _, present := obj[name.(string)]; !present {
				*out = append(*out, fmt.Sprintf("%s: misses required field %s", at, name))
			}
		}
	}
	extra, hasExtra := s["additionalProperties"]
	names := make([]string, 0, len(obj))
	for name := range obj {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		item := obj[name]
		if c.fields && isZero(item) {
			continue
		}
		where := at + "." + name
		if prop, defined := properties[name]; defined {
			c.visit(prop, item, where, out)
		} else if extra == false || (!hasExtra && hasProps) {
			*out = append(*out, where+": is not in the spec")
		} else if extraSchema, ok := extra.(map[string]any); ok {
			c.visit(extraSchema, item, where, out)
		}
	}
}

// Example is Response for op's success status.
func Example(t testing.TB, op string, overlay map[string]any) map[string]any {
	t.Helper()
	status, err := SuccessStatus(op)
	if err != nil {
		t.Fatal(err)
	}
	return Response(t, op, status, overlay)
}

// Response builds the body of op's response with the given status from the
// spec: every field its schema defines, set to the schema's example when that
// example matches the schema, with overlay merged on top. An object in overlay
// is merged field by field; an array replaces the built one, each of its
// objects merged onto the schema's item. The test fails unless the result
// matches the schema, so an overlay cannot add a field Composio does not send.
func Response(t testing.TB, op string, status int, overlay map[string]any) map[string]any {
	t.Helper()
	schema, err := responseSchema(op, status)
	if err != nil {
		t.Fatal(err)
	}
	b := builder{checker: checker{schemas: spec().Components.Schemas}}
	var o any
	if overlay != nil {
		o = overlay
	}
	body, ok := b.build(schema, o, overlay != nil, 0).(map[string]any)
	if !ok {
		t.Fatalf("composiotest: the %d response of %s is not an object", status, op)
	}
	if err := Check(op, status, body); err != nil {
		t.Fatal(err)
	}
	return body
}

type builder struct{ checker }

// clone copies a value decoded from the embedded spec, which every test
// shares, so callers can edit what Example returns.
func clone(v any) any {
	switch v := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, item := range v {
			out[k] = clone(item)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = clone(item)
		}
		return out
	}
	return v
}

func (b builder) matches(schema, v any) bool {
	var problems []string
	b.visit(schema, v, "$", &problems)
	return len(problems) == 0
}

func (b builder) build(rawSchema, overlay any, has bool, depth int) any {
	s := b.resolve(rawSchema)
	if s == nil || depth > 24 {
		return overlay
	}
	_, overlayIsMap := overlay.(map[string]any)
	_, overlayIsList := overlay.([]any)
	if has && !overlayIsMap && !overlayIsList {
		return overlay
	}
	if example, ok := s["example"]; ok && !has && b.matches(s, example) {
		return clone(example)
	}
	alternatives, _ := s["anyOf"].([]any)
	if alternatives == nil {
		alternatives, _ = s["oneOf"].([]any)
	}
	if _, typed := s["type"]; len(alternatives) > 0 && !typed && s["properties"] == nil {
		for _, branch := range alternatives {
			if v := b.build(branch, overlay, has, depth+1); b.matches(branch, v) {
				return v
			}
		}
		return b.build(alternatives[0], overlay, has, depth+1)
	}
	typ, _ := s["type"].(string)
	properties, _ := s["properties"].(map[string]any)
	switch {
	case typ == "object" || properties != nil:
		fields, _ := overlay.(map[string]any)
		if properties == nil && has {
			return overlay
		}
		out := map[string]any{}
		for name, prop := range properties {
			value, set := fields[name]
			out[name] = b.build(prop, value, set, depth+1)
		}
		for name, value := range fields {
			if _, defined := properties[name]; !defined {
				out[name] = value
			}
		}
		return out
	case typ == "array":
		if items, ok := overlay.([]any); ok {
			out := make([]any, len(items))
			for i, item := range items {
				out[i] = b.build(s["items"], item, true, depth+1)
			}
			return out
		}
		return []any{b.build(s["items"], nil, false, depth+1)}
	}
	if enum, ok := s["enum"].([]any); ok && len(enum) > 0 {
		return enum[0]
	}
	switch typ {
	case "string":
		return "string"
	case "number", "integer":
		return float64(1)
	case "boolean":
		return false
	}
	return nil
}
