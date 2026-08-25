package projectauth

import "errors"

var (
	ErrDisabled               = errors.New("project permissions are disabled")
	ErrNotWorkspaceMember     = errors.New("user is not a workspace member")
	ErrNoProjectAccess        = errors.New("user is not a project member")
	ErrForbidden              = errors.New("project permission denied")
	ErrInvalidRole            = errors.New("invalid project role")
	ErrInvalidIssuePermission = errors.New("invalid issue permission")
	ErrCrossWorkspace         = errors.New("project member is outside the project workspace")
	ErrInvalidReportFilter    = errors.New("invalid permission report filter")
)
