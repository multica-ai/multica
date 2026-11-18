package kagentbroker

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"unicode/utf8"
)

var errDenied = errors.New("broker: denied")

const bodyLimit = 64 << 10
const responseLimit = 1 << 20

// parseObject rejects ambiguity before any typed decoding (encoding/json alone
// accepts duplicate and case-insensitive keys). Depth and bytes are bounded.
func parseObject(raw []byte) (map[string]any, error) {
	if !utf8.Valid(raw) {
		return nil, errDenied
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	v, err := value(d, 0)
	if err != nil {
		return nil, errDenied
	}
	if _, err = d.Token(); err != io.EOF {
		return nil, errDenied
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, errDenied
	}
	return m, nil
}
func value(d *json.Decoder, depth int) (any, error) {
	if depth > 20 {
		return nil, errDenied
	}
	t, err := d.Token()
	if err != nil {
		return nil, err
	}
	switch t {
	case json.Delim('{'):
		m := map[string]any{}
		for d.More() {
			k, err := d.Token()
			if err != nil {
				return nil, err
			}
			s, ok := k.(string)
			if !ok {
				return nil, errDenied
			}
			if _, exists := m[s]; exists {
				return nil, errDenied
			}
			v, err := value(d, depth+1)
			if err != nil {
				return nil, err
			}
			m[s] = v
		}
		end, err := d.Token()
		if err != nil || end != json.Delim('}') {
			return nil, errDenied
		}
		return m, nil
	case json.Delim('['):
		a := []any{}
		for d.More() {
			v, err := value(d, depth+1)
			if err != nil {
				return nil, err
			}
			a = append(a, v)
		}
		end, err := d.Token()
		if err != nil || end != json.Delim(']') {
			return nil, errDenied
		}
		return a, nil
	}
	if _, ok := t.(json.Delim); ok {
		return nil, errDenied
	}
	return t, nil
}
func object(v any) map[string]any { m, _ := v.(map[string]any); return m }
func str(v any) string            { s, _ := v.(string); return s }
func keys(m map[string]any, required []string, optional ...string) bool {
	if m == nil {
		return false
	}
	allowed := map[string]bool{}
	for _, k := range required {
		allowed[k] = true
		if _, ok := m[k]; !ok {
			return false
		}
	}
	for _, k := range optional {
		allowed[k] = true
	}
	for k := range m {
		if !allowed[k] {
			return false
		}
	}
	return true
}
func boundedRead(r io.Reader, n int64) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, n+1))
	if err != nil || int64(len(b)) > n {
		return nil, errDenied
	}
	return b, nil
}
