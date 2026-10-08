package vocab

import (
	"strings"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// Actor is who performs an operation (audit, authorization): a party (person, organization or
// service account) or the system itself. The request-scoped actor travels in the context
// (application.WithActor / application.ActorFrom) instead of the C# ambient AsyncLocal.
type Actor struct {
	PartyID domain.UUID `json:"partyId,omitempty"`
	Name    string      `json:"name"`
}

// SystemActor is the actor of operations not triggered by a party (jobs, migrations, relays).
var SystemActor = Actor{Name: "System"}

// NewActor creates an actor for a party.
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

// IsSystem reports whether the actor is the system.
func (a Actor) IsSystem() bool { return a.PartyID.IsZero() }

// IsZero reports whether the actor is absent.
func (a Actor) IsZero() bool { return a == Actor{} }

// String returns "Name (id)" or "System".
func (a Actor) String() string {
	if a.IsSystem() {
		return a.Name
	}
	return a.Name + " (" + a.PartyID.String() + ")"
}
