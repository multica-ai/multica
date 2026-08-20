package tagaccess_test

import (
	"reflect"
	"slices"
	"testing"

	"github.com/multica-ai/multica/server/internal/tagaccess"
)

func TestAuthorizationMutationIsExposedOnlyByGate(t *testing.T) {
	gate := tagaccess.New(tagaccess.NewMemoryStore(), tagaccess.SystemClock{}, tagaccess.NewFixtureDeliveryVerifier())
	authenticated, err := tagaccess.NewAuthenticatedAccess(tagaccess.NewMemoryStore(), tagaccess.SystemClock{}, map[string][]byte{
		"vibes-primary": []byte("vibes-authority-test-key-32-bytes-minimum"),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ingress := authenticated.Ingress
	tests := []struct {
		name  string
		value any
		want  []string
	}{
		{
			name:  "Gate",
			value: gate,
			want:  []string{"ApplyAuthorityDelivery", "ApplyProjection", "Authorize", "GrantSession"},
		},
		{
			name:  "authenticated authority ingress",
			value: ingress,
			want:  []string{"Deliver"},
		},
		{
			name:  "authenticated identity restriction ingress",
			value: authenticated.IdentityIngress,
			want:  []string{"Deliver"},
		},
		{
			name:  "authenticated session Workspace supersession ingress",
			value: authenticated.SessionWorkspaceIngress,
			want:  []string{"Deliver"},
		},
		{
			name:  "Postgres adapter",
			value: tagaccess.NewPostgresStore(nil),
			want:  nil,
		},
		{
			name:  "Memory fixture adapter",
			value: tagaccess.NewMemoryStore(),
			want:  []string{"SetFailure"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := exportedMethodNames(test.value)
			if !slices.Equal(got, test.want) {
				t.Fatalf("exported methods = %v, want %v", got, test.want)
			}
		})
	}
}

func exportedMethodNames(value any) []string {
	typeOf := reflect.TypeOf(value)
	methods := make([]string, 0, typeOf.NumMethod())
	for index := 0; index < typeOf.NumMethod(); index++ {
		methods = append(methods, typeOf.Method(index).Name)
	}
	return methods
}
