package infrastructure

import (
	"context"

	"github.com/jhermoso/karpo-fw-go/contexts/facilities/contracts"
	papp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// FacilitiesDirectory adapta el directorio de Facilities al puerto de Parties (ACL): Parties
// guarda el id de la instalación de sus roles en instalaciones y pregunta a Facilities de quién es
// y si está activa.
type FacilitiesDirectory struct{ Directory contracts.Directory }

// Facilities implementa papp.FacilityDirectory.
func (f FacilitiesDirectory) Facilities(ctx context.Context, ids []fw.UUID) (map[fw.UUID]papp.FacilityInfo, error) {
	want := make([]string, len(ids))
	for i, id := range ids {
		want[i] = id.String()
	}
	refs, err := f.Directory.Resolve(ctx, want)
	if err != nil {
		return nil, err
	}
	out := make(map[fw.UUID]papp.FacilityInfo, len(refs))
	for _, r := range refs {
		out[fw.MustParseUUID(r.ID)] = papp.FacilityInfo{Organization: fw.MustParseUUID(r.Organization), Name: r.Name, Active: r.Active}
	}
	return out, nil
}
