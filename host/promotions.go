package host

import (
	"context"

	exgdomain "github.com/jhermoso/karpo-fw-go/contexts/exchange/domain"
	parcontracts "github.com/jhermoso/karpo-fw-go/contexts/parties/contracts"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// PromotionCodes dice a Exchange de quién es un código de promoción: del colaborador de la empresa
// que lo tiene en Parties, en su relación de colaborador. Un código del colaborador de otra empresa
// aquí no es de nadie.
type PromotionCodes struct{ Collaborators parcontracts.Collaborators }

var _ exgdomain.Collaborators = PromotionCodes{}

// ByPromotionCode implementa Collaborators de Exchange.
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
