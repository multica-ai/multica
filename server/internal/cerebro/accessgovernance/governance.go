// Package accessgovernance identifies and expires access that should be
// tightened. It has no operation that can create or widen access.
package accessgovernance

import (
	"sort"
	"time"
)

type Grant struct {
	ID             string
	RoleID         string
	SubjectType    string
	SubjectID      string
	CapabilityID   string
	OwnerID        string
	AddedAt        time.Time
	ExpiresAt      *time.Time
	LastUsedAt     *time.Time
	SubjectPresent bool
}

type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityHigh     Severity = "high"
	SeverityMedium   Severity = "medium"
)

type Finding struct {
	GrantID        string
	RoleID         string
	SubjectType    string
	SubjectID      string
	BindingAddedAt time.Time
	Severity       Severity
	Reason         string
	TightenOnly    bool
}

// Scan reports expired bindings, orphaned subjects, and access unused
// for longer than unusedAfter. It never returns an allow/open action.
func Scan(now time.Time, unusedAfter time.Duration, grants []Grant) []Finding {
	findings := make([]Finding, 0)
	for _, grant := range grants {
		finding := func(severity Severity, reason string) Finding {
			return Finding{
				GrantID: grant.ID, RoleID: grant.RoleID, SubjectType: grant.SubjectType,
				SubjectID: grant.SubjectID, BindingAddedAt: grant.AddedAt,
				Severity: severity, Reason: reason, TightenOnly: true,
			}
		}
		switch {
		case !grant.SubjectPresent:
			findings = append(findings, finding(SeverityCritical, "orphaned access binding"))
		case grant.ExpiresAt != nil && !grant.ExpiresAt.After(now):
			findings = append(findings, finding(SeverityHigh, "access binding expired"))
		case unusedAfter > 0 && grant.LastUsedAt != nil && now.Sub(*grant.LastUsedAt) >= unusedAfter:
			findings = append(findings, finding(SeverityMedium, "access has not been used within the review window"))
		case unusedAfter > 0 && grant.LastUsedAt == nil && !grant.AddedAt.IsZero() && now.Sub(grant.AddedAt) >= unusedAfter:
			findings = append(findings, finding(SeverityMedium, "access has never been used within the review window"))
		}
	}
	sort.SliceStable(findings, func(i, j int) bool {
		rank := map[Severity]int{SeverityCritical: 0, SeverityHigh: 1, SeverityMedium: 2}
		if rank[findings[i].Severity] != rank[findings[j].Severity] {
			return rank[findings[i].Severity] < rank[findings[j].Severity]
		}
		return findings[i].GrantID < findings[j].GrantID
	})
	return findings
}
