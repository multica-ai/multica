package handler

import "testing"

// TestFolderActionAllowed_Pure exercises the pure predicate behind the folder
// action guard (delete/rename/move). It runs without a database. The guard must:
//   - allow everyone when the guard is OFF (the default, legacy behavior)
//   - allow everyone on an unowned (legacy) folder even when on
//   - allow the folder owner and workspace owners/admins when on
//   - deny any other member when on
func TestFolderActionAllowed_Pure(t *testing.T) {
	const owner = "11111111-1111-1111-1111-111111111111"
	const other = "22222222-2222-2222-2222-222222222222"

	cases := []struct {
		name         string
		ownerID      string
		guardEnabled bool
		callerID     string
		callerAdmin  bool
		want         bool
	}{
		{"guard off: other member may act", owner, false, other, false, true},
		{"guard off: even with no owner", "", false, other, false, true},
		{"guard on: unowned folder is open", "", true, other, false, true},
		{"guard on: owner may act", owner, true, owner, false, true},
		{"guard on: admin may act", owner, true, other, true, true},
		{"guard on: owner who is also admin", owner, true, owner, true, true},
		{"guard on: other member denied", owner, true, other, false, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := folderActionAllowed(tc.ownerID, tc.guardEnabled, tc.callerID, tc.callerAdmin)
			if got != tc.want {
				t.Fatalf("folderActionAllowed(owner=%q, enabled=%v, caller=%q, admin=%v) = %v, want %v",
					tc.ownerID, tc.guardEnabled, tc.callerID, tc.callerAdmin, got, tc.want)
			}
		})
	}
}
