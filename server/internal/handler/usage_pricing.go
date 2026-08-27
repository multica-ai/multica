package handler

import (
	_ "embed"
	"encoding/json"
	"net/http"
)

// usagePricingV1Raw is the canonical pricing protocol document. The generated
// TypeScript module consumed by Multica's own clients comes from this same file.
//
//go:embed usage_pricing_v1.json
var usagePricingV1Raw []byte

var usagePricingV1 any

func init() {
	if err := json.Unmarshal(usagePricingV1Raw, &usagePricingV1); err != nil {
		panic("invalid embedded usage pricing contract: " + err.Error())
	}
}

// GetUsagePricing returns the versioned model-rate contract used for estimates.
func (h *Handler) GetUsagePricing(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	writeJSON(w, http.StatusOK, usagePricingV1)
}
