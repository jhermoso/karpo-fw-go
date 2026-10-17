package vocab

import (
	"strings"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// Actor es quien hace una operación (auditoría, autorización): una party (persona, organización o
// cuenta de servicio) o el propio sistema. El actor de la petición viaja en el contexto
// (application.WithActor / application.ActorFrom) en lugar del AsyncLocal ambiente de C#.
type Actor struct {
	PartyID domain.UUID `json:"partyId,omitempty"`
	Name    string      `json:"name"`
}

// SystemActor es el actor de las operaciones que no lanza una party (trabajos, migraciones, relays).
var SystemActor = Actor{Name: "System"}

// NewActor crea un actor para una party.
func NewActor(partyID domain.UUID, name string) (Actor, error) {
	name = strings.TrimSpace(name)
	var v domain.Validation
	v.Require(!partyID.IsZero(), "partyId", "required", "party id is required")
	v.Require(name != "", "name", "required", "actor name is required")
	if err := v.Err(); err != nil {
		return Actor{}, err
	}
	return Actor{PartyID: partyID, Name: name}, nil
}

// IsSystem dice si el actor es el sistema.
func (a Actor) IsSystem() bool { return a.PartyID.IsZero() }

// IsZero dice si falta el actor.
func (a Actor) IsZero() bool { return a == Actor{} }

// String devuelve "Name (id)" o "System".
func (a Actor) String() string {
	if a.IsSystem() {
		return a.Name
	}
	return a.Name + " (" + a.PartyID.String() + ")"
}
