package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// ValidateKagentRuntimeConfig makes native text execution an explicit per-agent
// choice. Endpoints, credentials and shared harness references are daemon-owned.
func ValidateKagentRuntimeConfig(raw []byte) error {
	var config struct {
		Mode string `json:"mode"`
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&config); err != nil || config.Mode != "native-text" {
		return fmt.Errorf("kagent: runtime_config must be {\"mode\":\"native-text\"}")
	}
	if d.Decode(new(any)) != io.EOF {
		return fmt.Errorf("kagent: invalid runtime_config")
	}
	return nil
}
