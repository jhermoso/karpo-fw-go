package host

import (
	"context"

	exgdomain "github.com/jhermoso/karpo-fw-go/contexts/exchange/domain"
	parcontracts "github.com/jhermoso/karpo-fw-go/contexts/parties/contracts"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// PromotionCodes tells Exchange whose a promotion code is: the collaborator of the company that
// has it in Parties, on its collaborator relationship. A code of the collaborator of another
// company is nobody's here.
type PromotionCodes struct{ Collaborators parcontracts.Collaborators }

var _ exgdomain.Collaborators = PromotionCodes{}

// ByPromotionCode implements Exchange's Collaborators.
func (p PromotionCodes) ByPromotionCode(ctx context.Context, company exgdomain.OrganizationID, code string) (exgdomain.PartyID, bool, error) {
	c, found, err := p.Collaborators.ByPromotionCode(ctx, company.String(), code)
	if err != nil || !found {
		return exgdomain.PartyID{}, false, err
	}
	id, err := fw.ParseUUID(c.PartyID)
	if err != nil {
		return exgdomain.PartyID{}, false, err
	}
	return exgdomain.PartyID{UUID: id}, true, nil
}
