package featureflags

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/pkg/featureflag"
)

func TestIssueLifecycleV1DefaultsOffAndCanBeEnabled(t *testing.T) {
	ctx := context.Background()
	if IssueLifecycleV1Enabled(ctx, nil) {
		t.Fatal("issue lifecycle canonical reads must default off")
	}
	provider := featureflag.NewStaticProvider()
	provider.Set(IssueLifecycleV1, featureflag.Rule{Default: true})
	if !IssueLifecycleV1Enabled(ctx, featureflag.NewService(provider)) {
		t.Fatal("issue lifecycle flag did not enable canonical reads")
	}
}
