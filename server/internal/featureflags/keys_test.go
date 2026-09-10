package featureflags

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/pkg/featureflag"
)

func TestIssueWorkflowV1DefaultsOffAndCanBeEnabled(t *testing.T) {
	ctx := context.Background()
	if IssueWorkflowV1Enabled(ctx, nil) {
		t.Fatal("issue workflow canonical reads must default off")
	}
	provider := featureflag.NewStaticProvider()
	provider.Set(IssueWorkflowV1, featureflag.Rule{Default: true})
	if !IssueWorkflowV1Enabled(ctx, featureflag.NewService(provider)) {
		t.Fatal("issue workflow flag did not enable canonical reads")
	}
}
