package media

import (
	"context"
	"fmt"
	"strings"

	"github.com/multica-ai/multica/server/pkg/agent"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type Antigravity struct {
	Command agent.Command
	Home    string
}

func (e *Antigravity) Probe(ctx context.Context) error {
	return agent.ProbeAntigravityImages(ctx, e.Command, e.Home)
}

func (e *Antigravity) Capabilities() []string { return []string{"media-image-agy-v1"} }

func (e *Antigravity) Submit(ctx context.Context, request protocol.MediaRequest, inputs []Input, dir string) (Artifact, error) {
	if request.Capability != "image-generate" && request.Capability != "image-edit" {
		return Artifact{}, &RequestError{Message: "unsupported Antigravity image operation"}
	}
	if request.Model != "" || request.Duration != 0 {
		return Artifact{}, &RequestError{Message: "Antigravity native image tools do not expose model or duration selection"}
	}
	if request.Capability == "image-edit" && len(inputs) == 0 {
		return Artifact{}, &RequestError{Message: "image editing requires a reference image"}
	}
	var prompt strings.Builder
	if request.Capability == "image-edit" {
		prompt.WriteString("Edit the supplied reference images.\n")
	}
	if request.Size != "" {
		fmt.Fprintf(&prompt, "Requested image size: %s.\n", request.Size)
	}
	if request.Aspect != "" {
		fmt.Fprintf(&prompt, "Requested aspect ratio: %s.\n", request.Aspect)
	}
	paths := make([]string, 0, len(inputs))
	for i, input := range inputs {
		paths = append(paths, input.Path)
		fmt.Fprintf(&prompt, "Reference image %d role: %s.\n", i+1, input.Role)
	}
	prompt.WriteString(request.Prompt)
	result, err := agent.GenerateAntigravityImage(ctx, e.Command, e.Home, dir, prompt.String(), paths)
	if err != nil {
		return Artifact{}, err
	}
	if result.RateLimited {
		return Artifact{}, &RateLimitError{}
	}
	return Artifact{Path: result.Path}, nil
}
