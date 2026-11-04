package service

import (
	"fmt"
	"testing"
)

func TestCapRowsMarksTruncation(t *testing.T) {
	rows := make([]int, OrgMapMaxSquads+1)
	kept, truncated := CapRows(rows, OrgMapMaxSquads)
	if len(kept) != OrgMapMaxSquads || !truncated {
		t.Fatalf("over the cap: len=%d truncated=%v", len(kept), truncated)
	}
	kept, truncated = CapRows(rows[:OrgMapMaxSquads], OrgMapMaxSquads)
	if len(kept) != OrgMapMaxSquads || truncated {
		t.Fatalf("exactly at the cap: len=%d truncated=%v", len(kept), truncated)
	}
	kept, truncated = CapRows([]int{1, 2}, OrgMapMaxSquads)
	if len(kept) != 2 || truncated {
		t.Fatalf("under the cap: len=%d truncated=%v", len(kept), truncated)
	}
}

func TestCapOrgMapMembersKeepsLeaderFirst(t *testing.T) {
	members := make([]OrgMapMember, 0, OrgMapMaxMembersPerSquad+3)
	members = append(members, OrgMapMember{Type: "agent", Name: "leader", IsLeader: true})
	for i := 0; i < OrgMapMaxMembersPerSquad+2; i++ {
		members = append(members, OrgMapMember{Type: "member", Name: fmt.Sprintf("m%d", i)})
	}
	kept, truncated := CapOrgMapMembers(members, OrgMapMaxMembersPerSquad)
	if !truncated || len(kept) != OrgMapMaxMembersPerSquad {
		t.Fatalf("len=%d truncated=%v", len(kept), truncated)
	}
	if !kept[0].IsLeader {
		t.Fatalf("leader was dropped or moved: first member %q", kept[0].Name)
	}
}

func TestOrgRuntimeOnline(t *testing.T) {
	cases := map[string]bool{"online": true, "offline": false, "": false, "Online": false}
	for status, want := range cases {
		if got := OrgRuntimeOnline(status); got != want {
			t.Errorf("OrgRuntimeOnline(%q) = %v, want %v", status, got, want)
		}
	}
}
