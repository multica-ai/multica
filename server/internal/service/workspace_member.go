package service

import "time"

const (
	MemberSourceManual = "manual"
	MemberSourceDept   = "dept"

	MemberStatusActive            = "active"
	MemberStatusPendingActivation = "pending_activation"
	MemberStatusInactive          = "inactive"
)

type WorkspaceDeptMemberSnapshot struct {
	MemberID            string
	UserID              string
	Source              string
	Status              string
	ExternalUserID      string
	ExternalUniversalID string
	Name                string
	EmployeeID          string
	DepartmentID        string
	DepartmentName      string
	DepartmentPath      string
	Position            string
	IsMainDepartment    bool
	DeptUserStatus      int
	LastSyncedAt        time.Time
}
