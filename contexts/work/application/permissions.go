package application

import "github.com/jhermoso/karpo-fw-go/pkg/application/authz"

// Permissions (the C# seeded WorkEffort.* codes no endpoint checked). Planning the work, moving
// its status, recording time and approving it are separate.
var (
	PermWorkRead     = authz.MustPermission("Work.Work.Read")
	PermWorkUpdate   = authz.MustPermission("Work.Work.Update")
	PermWorkProgress = authz.MustPermission("Work.Work.Progress")
	PermTimeRead     = authz.MustPermission("Work.Time.Read")
	PermTimeRecord   = authz.MustPermission("Work.Time.Record")
	PermTimeApprove  = authz.MustPermission("Work.Time.Approve")
)

// Permissions returns the permissions this context declares to the Security catalog.
func Permissions() []authz.Permission {
	return []authz.Permission{PermWorkRead, PermWorkUpdate, PermWorkProgress, PermTimeRead, PermTimeRecord, PermTimeApprove}
}
