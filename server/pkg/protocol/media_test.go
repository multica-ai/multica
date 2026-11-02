package protocol

import "testing"

func TestMediaRequestImageOperations(t *testing.T) {
	for _, provider := range []string{"auto", "codex", "agy"} {
		for _, capability := range []string{"image-generate", "image-edit"} {
			r := MediaRequest{Provider: provider, Capability: capability, Prompt: "image"}
			if capability == "image-edit" {
				if r.Validate() == nil {
					t.Fatal("edit without a reference accepted")
				}
				r.Inputs = []MediaInput{{AttachmentID: "reference"}}
			}
			if err := r.Validate(); err != nil {
				t.Fatalf("%s/%s: %v", provider, capability, err)
			}
		}
	}
}

func TestMediaRetiredRequestsRejected(t *testing.T) {
	for _, provider := range []string{"auto", "codex", "agy", "flow", "whisk"} {
		for _, capability := range []string{"image-generate", "image-edit", "image-remix", "video-generate"} {
			if provider != "flow" && provider != "whisk" && (capability == "image-generate" || capability == "image-edit") {
				continue
			}
			r := MediaRequest{Provider: provider, Capability: capability, Prompt: "image", Inputs: []MediaInput{{AttachmentID: "reference"}}}
			if r.Validate() == nil || MediaCapabilityTag(provider, capability) != "" {
				t.Fatalf("retired request accepted: %s/%s", provider, capability)
			}
		}
	}
	if (MediaRequest{Provider: "auto", Capability: "image-generate", Prompt: "image", Duration: 1}).Validate() == nil {
		t.Fatal("video duration accepted on image request")
	}
}
