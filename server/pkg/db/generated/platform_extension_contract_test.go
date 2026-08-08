package db

import (
	"reflect"
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
