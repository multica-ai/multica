package db

import (
	"reflect"
	"strings"
	"testing"
)

func TestPlatformExtensionReleaseQueriesAreGenerated(t *testing.T) {
	typeOfQueries := reflect.TypeOf((*Queries)(nil))
	for _, name := range []string{
		"CreatePlatformExtensionReleaseReservation",
		"CompletePlatformExtensionRelease",
		"GetPlatformExtensionReleaseByIdentity",
		"GetPlatformExtensionReleaseInWorkspace",
		"ListPlatformExtensionReleasesInWorkspace",
	} {
		if _, ok := typeOfQueries.MethodByName(name); !ok {
			t.Errorf("sqlc did not generate %s", name)
		}
	}
}

func TestCompletePlatformExtensionReleaseIsOneShot(t *testing.T) {
	for _, guard := range []string{"runtime_id IS NULL", "squad_id IS NULL"} {
		if !strings.Contains(completePlatformExtensionRelease, guard) {
			t.Errorf("completion query is missing one-shot guard %q", guard)
		}
	}
}
