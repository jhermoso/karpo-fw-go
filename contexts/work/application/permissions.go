package application

import "github.com/jhermoso/karpo-fw-go/pkg/application/authz"

// Permisos (el C# sembraba códigos WorkEffort.* que ningún endpoint comprobaba). Planificar el
// trabajo, mover su estado, anotar horas y aprobarlas son permisos distintos.
var (
	PermWorkRead     = authz.MustPermission("Work.Work.Read")
	PermWorkUpdate   = authz.MustPermission("Work.Work.Update")
	PermWorkProgress = authz.MustPermission("Work.Work.Progress")
	PermTimeRead     = authz.MustPermission("Work.Time.Read")
	PermTimeRecord   = authz.MustPermission("Work.Time.Record")
	PermTimeApprove  = authz.MustPermission("Work.Time.Approve")
)

// Permissions devuelve los permisos que este contexto declara al catálogo de Security.
func Permissions() []authz.Permission {
	return []authz.Permission{PermWorkRead, PermWorkUpdate, PermWorkProgress, PermTimeRead, PermTimeRecord, PermTimeApprove}
}
