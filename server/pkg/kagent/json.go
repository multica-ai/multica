package kagent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Go's decoder accepts repeated members (including case aliases for struct
// fields). Reject them before decoding anything that can authorize a retry.
func uniqueJSON(raw []byte) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var value func() error
	value = func() error {
		t, err := d.Token()
		if err != nil {
			return err
		}
		switch t {
		case json.Delim('{'):
			seen := map[string]bool{}
			for d.More() {
				key, err := d.Token()
				if err != nil {
					return err
				}
				name, ok := key.(string)
				if !ok || seen[strings.ToLower(name)] {
					return fmt.Errorf("kagent: ambiguous JSON object")
				}
				seen[strings.ToLower(name)] = true
				if err := value(); err != nil {
					return err
				}
			}
			end, err := d.Token()
			if err != nil || end != json.Delim('}') {
				return fmt.Errorf("kagent: malformed JSON object")
			}
		case json.Delim('['):
			for d.More() {
				if err := value(); err != nil {
					return err
				}
			}
			end, err := d.Token()
			if err != nil || end != json.Delim(']') {
				return fmt.Errorf("kagent: malformed JSON array")
			}
		case json.Delim('}'), json.Delim(']'):
			return fmt.Errorf("kagent: malformed JSON")
		}
		return nil
	}
	if err := value(); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return fmt.Errorf("kagent: trailing JSON")
	}
	return nil
}
