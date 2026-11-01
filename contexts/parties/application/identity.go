package application

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/application/pipeline"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// Identidad y perfil de cada empresa (docs/DECISIONES-COMUNES.md, D-121 y D-122; docs/PARTIES.md
// §23): el idioma preferido, quién corrige el documento y el nombre, los contactos con dueña y, en
// la pareja, el número de proveedor, los códigos externos y los consentimientos de protección de
// datos.

// SetPreferredLanguage fija el idioma preferido de una party (ISO 639-1); vacío lo quita.
type SetPreferredLanguage struct {
	ID       domain.PartyID `json:"-"`
	Language string         `json:"language"`
}

// MarkPrimaryIdentification hace principal uno de los documentos de identidad de una party.
type MarkPrimaryIdentification struct {
	PartyID domain.PartyID
	ID      domain.IdentificationID
}

// SetSupplierNumber da a un proveedor su número en la organización, lo cambia o (vacío) se lo quita.
type SetSupplierNumber struct {
	ID             domain.RelationshipID `json:"-"`
	SupplierNumber string                `json:"supplierNumber"`
}

// SetExternalCodes sustituye los códigos externos de una pareja de cliente o de proveedor (D-29);
// uno vacío se quita.
type SetExternalCodes struct {
	ID           domain.RelationshipID `json:"-"`
	SageCode     string                `json:"sageCode"`
	ApiscoreCode string                `json:"apiscoreCode"`
}

// RecordConsent registra un consentimiento de protección de datos en una pareja (D-122).
type RecordConsent struct {
	ID              domain.RelationshipID `json:"-"`
	LegalBasis      string                `json:"legalBasis"`
	Purpose         string                `json:"purpose"`
	GrantedAt       *time.Time            `json:"grantedAt,omitempty"` // ahora si falta
	RetentionMonths int                   `json:"retentionMonths,omitempty"`
	Objection       bool                  `json:"objection,omitempty"`
	Notes           string                `json:"notes,omitempty"`
}

// WithdrawConsent retira un consentimiento (ahora si At es nil).
type WithdrawConsent struct {
	ID        domain.RelationshipID `json:"-"`
	ConsentID domain.ConsentID      `json:"-"`
	At        *time.Time            `json:"at,omitempty"`
}

// ConsentDTO es un consentimiento de protección de datos de una pareja.
type ConsentDTO struct {
	ID              string     `json:"id"`
	LegalBasis      string     `json:"legalBasis"`
	Purpose         string     `json:"purpose"`
	GrantedAt       time.Time  `json:"grantedAt"`
	WithdrawnAt     *time.Time `json:"withdrawnAt,omitempty"`
	RetentionMonths int        `json:"retentionMonths,omitempty"`
	Objection       bool       `json:"objection,omitempty"`
	Notes           string     `json:"notes,omitempty"`
	Active          bool       `json:"active"`
}

// SupplierDTO son los datos de una relación de proveedor.
type SupplierDTO struct {
	Number       string `json:"number,omitempty"`
	SageCode     string `json:"sageCode,omitempty"`
	ApiscoreCode string `json:"apiscoreCode,omitempty"`
}

// parseLanguage valida un idioma ISO 639-1; vacío es «no se sabe».
func parseLanguage(s string) (vocab.LanguageCode, error) {
	if strings.TrimSpace(s) == "" {
		return vocab.LanguageCode{}, nil
	}
	l, err := vocab.NewLanguageCode(s)
	if err != nil {
		var v fw.Validation
		v.Add("language", "iso639", "language must be an ISO 639-1 code (es, en, ca...)")
		return vocab.LanguageCode{}, v.Err()
	}
	return l, nil
}

// withLanguage da a la party que construye build su idioma preferido.
func withLanguage(l vocab.LanguageCode, build func(domain.PartyID) (*domain.Party, error)) func(domain.PartyID) (*domain.Party, error) {
	return func(id domain.PartyID) (*domain.Party, error) {
		p, err := build(id)
		if err != nil || l.IsZero() {
			return p, err
		}
		return p, p.SetPreferredLanguage(l)
	}
}

func consentDTOs(r *domain.Relationship) []ConsentDTO {
	cs := r.Consents()
	if len(cs) == 0 {
		return nil
	}
	out := make([]ConsentDTO, len(cs))
	for i, c := range cs {
		out[i] = ConsentDTO{ID: c.ID.String(), LegalBasis: string(c.LegalBasis), Purpose: c.Purpose, GrantedAt: c.GrantedAt,
			WithdrawnAt: c.WithdrawnAt, RetentionMonths: c.RetentionMonths, Objection: c.Objection, Notes: c.Notes, Active: c.IsActive()}
	}
	return out
}

// companyOf dice cuál de los dos lados de una relación es la empresa del grupo, por los roles de su
// tipo: el lado que pide Internal Organization (o un rol por debajo). Si los dos lo piden, el
// destino.
func (s service) companyOf(ctx context.Context, r *domain.Relationship) (domain.PartyID, error) {
	types, err := s.relationshipTypes(ctx)
	if err != nil {
		return domain.PartyID{}, err
	}
	cat, err := s.roleCatalog(ctx)
	if err != nil {
		return domain.PartyID{}, err
	}
	rt := types[r.Type()]
	switch {
	case cat.IsA(rt.ToRole, domain.RoleInternalOrganization):
		return r.To(), nil
	case cat.IsA(rt.FromRole, domain.RoleInternalOrganization):
		return r.From(), nil
	}
	return domain.PartyID{}, fw.Violation("parties.consent_needs_company",
		"a data protection consent is given to a company: the relationship has no internal organization side")
}

// addIdentityAndProfile conecta los casos de uso de D-122 que no caben en los que ya había.
func addIdentityAndProfile(svc *Service, s service) {
	retry, backoff := 3, 10*time.Millisecond

	svc.MarkPrimary = chain(PermPartyUpdate, func(ctx context.Context, c MarkPrimaryIdentification) (PartyDTO, error) {
		return s.updateParty(ctx, c.PartyID, func(p *domain.Party, _ *domain.Catalog) error {
			if err := s.scope(ctx).mayCorrectIdentity(p); err != nil {
				return err
			}
			return p.MarkPrimaryIdentification(c.ID)
		})
	}, pipeline.RetryOnConflict[MarkPrimaryIdentification, PartyDTO](retry, backoff))

	// El número de proveedor sigue las reglas del número de cliente (docs/PARTIES.md §9): lo da quien
	// escribe en la empresa que compra (el origen de la relación de proveedor), y no se da a otro
	// proveedor de esa empresa mientras una relación del primero, vigente o terminada, lo lleve.
	svc.SetSupplierNumber = chain(PermRelationshipUpdate, func(ctx context.Context, c SetSupplierNumber) (RelationshipDTO, error) {
		number := domain.NormalizeCustomerNumber(c.SupplierNumber)
		r, _, _, err := s.operableRelationship(ctx, c.ID)
		if err != nil {
			return RelationshipDTO{}, err
		}
		types, err := s.relationshipTypes(ctx)
		if err != nil {
			return RelationshipDTO{}, err
		}
		if types[r.Type()].Code != domain.CodeSupplier { // antes que el permiso: el origen sólo es la empresa en un proveedor
			return RelationshipDTO{}, fw.Violation("parties.not_a_supplier_relationship", "only a supplier relationship has a supplier number")
		}
		if sc := s.scope(ctx); !sc.global && !sc.canWriteOrg(r.From()) {
			return RelationshipDTO{}, fmt.Errorf("%w: the supplier number is given by the organization that numbers its suppliers", fw.ErrForbidden)
		}
		if number != "" {
			same, err := s.relationships.Repository().Find(ctx, spec.And(domain.RelFieldType.Eq(r.Type()), domain.RelFieldFrom.Eq(r.From()),
				domain.RelFieldSupplierNumber.Eq(number)))
			if err != nil {
				return RelationshipDTO{}, err
			}
			for _, o := range same {
				if o.To() != r.To() {
					return RelationshipDTO{}, fw.Violation("parties.duplicate_supplier_number",
						"another supplier of the organization has, or had, that supplier number")
				}
			}
		}
		return s.changeDetails(ctx, c.ID, func(r *domain.Relationship, rt domain.RelationshipType) error {
			return r.SetSupplierNumber(rt, number)
		})
	}, pipeline.RetryOnConflict[SetSupplierNumber, RelationshipDTO](retry, backoff))

	// Los códigos externos son de la pareja: los cambia quien escribe en la empresa de la pareja.
	svc.SetExternalCodes = chain(PermRelationshipUpdate, func(ctx context.Context, c SetExternalCodes) (RelationshipDTO, error) {
		r, _, _, err := s.operableRelationship(ctx, c.ID)
		if err != nil {
			return RelationshipDTO{}, err
		}
		types, err := s.relationshipTypes(ctx)
		if err != nil {
			return RelationshipDTO{}, err
		}
		company := r.To() // cliente: la empresa vende (destino)
		switch types[r.Type()].Code {
		case domain.CodeSupplier:
			company = r.From() // proveedor: la empresa compra (origen)
		case domain.CodeCustomer:
		default:
			return RelationshipDTO{}, fw.Violation("parties.not_a_pair_relationship", "only a customer or supplier relationship has external codes")
		}
		if sc := s.scope(ctx); !sc.global && !sc.canWriteOrg(company) {
			return RelationshipDTO{}, fmt.Errorf("%w: the external codes belong to the company of the pair", fw.ErrForbidden)
		}
		return s.changeDetails(ctx, c.ID, func(r *domain.Relationship, rt domain.RelationshipType) error {
			return r.SetExternalCodes(rt, domain.ExternalCodes{Sage: c.SageCode, Apiscore: c.ApiscoreCode})
		})
	}, pipeline.RetryOnConflict[SetExternalCodes, RelationshipDTO](retry, backoff))

	// Un consentimiento es de la empresa, responsable del tratamiento: lo registra y lo retira quien
	// escribe en ella, con el permiso de cambiar los datos de una relación (decidido en esta sesión).
	svc.RecordConsent = chain(PermRelationshipUpdate, func(ctx context.Context, c RecordConsent) (RelationshipDTO, error) {
		basis, err := domain.ParseLegalBasis(c.LegalBasis)
		if err != nil {
			return RelationshipDTO{}, err
		}
		r, _, _, err := s.operableRelationship(ctx, c.ID)
		if err != nil {
			return RelationshipDTO{}, err
		}
		org, err := s.companyOf(ctx, r)
		if err != nil {
			return RelationshipDTO{}, err
		}
		if sc := s.scope(ctx); !sc.global && !sc.canWriteOrg(org) {
			return RelationshipDTO{}, fmt.Errorf("%w: the consent is recorded by the company of the pair", fw.ErrForbidden)
		}
		d := domain.ConsentData{LegalBasis: basis, Purpose: c.Purpose, GrantedAt: nowOr(c.GrantedAt), RetentionMonths: c.RetentionMonths,
			Objection: c.Objection, Notes: c.Notes}
		return s.changeDetails(ctx, c.ID, func(r *domain.Relationship, _ domain.RelationshipType) error {
			_, err := r.RecordConsent(org, d)
			return err
		})
	}, pipeline.RetryOnConflict[RecordConsent, RelationshipDTO](retry, backoff))

	// Retirarlo se puede aunque la relación haya terminado o el tercero esté fusionado en otro: es un
	// derecho del tercero.
	svc.WithdrawConsent = chain(PermRelationshipUpdate, func(ctx context.Context, c WithdrawConsent) (RelationshipDTO, error) {
		types, err := s.relationshipTypes(ctx)
		if err != nil {
			return RelationshipDTO{}, err
		}
		r, _, _, err := s.writableRelationship(ctx, c.ID)
		if err != nil {
			return RelationshipDTO{}, err
		}
		org, err := s.companyOf(ctx, r)
		if err != nil {
			return RelationshipDTO{}, err
		}
		if sc := s.scope(ctx); !sc.global && !sc.canWriteOrg(org) {
			return RelationshipDTO{}, fmt.Errorf("%w: the consent is withdrawn by the company of the pair", fw.ErrForbidden)
		}
		r, err = s.relationships.Update(ctx, c.ID, func(_ context.Context, r *domain.Relationship) error {
			return r.WithdrawConsent(org, c.ConsentID, nowOr(c.At))
		})
		if err != nil {
			return RelationshipDTO{}, err
		}
		return s.relationshipDTO(ctx, r, types), nil
	}, pipeline.RetryOnConflict[WithdrawConsent, RelationshipDTO](retry, backoff))
}

// relationshipDTO traduce una relación para quien llama. El perfil de la pareja (los números de
// cliente y de proveedor, los códigos externos y los consentimientos) es de la empresa de la pareja
// (D-122): si quien llama no la tiene en su ámbito (Beta, cliente de Acme, mirando su relación con
// Acme), no lo ve.
func (s service) relationshipDTO(ctx context.Context, r *domain.Relationship, types map[domain.RelationshipTypeID]domain.RelationshipType) RelationshipDTO {
	d := RelationshipToDTO(r, types)
	sc := s.scope(ctx)
	if sc.global {
		return d
	}
	var company domain.PartyID
	switch types[r.Type()].Code {
	case domain.CodeCustomer:
		company = r.To()
	case domain.CodeSupplier:
		company = r.From()
	default:
		if d.DataProtectionConsents == nil {
			return d
		}
		var err error
		if company, err = s.companyOf(ctx, r); err != nil {
			d.DataProtectionConsents = nil
			return d
		}
	}
	if sc.seesOwned(company) {
		return d
	}
	d.DataProtectionConsents = nil
	if d.Customer != nil {
		d.Customer = &CustomerDTO{}
	}
	if d.Supplier != nil {
		d.Supplier = &SupplierDTO{}
	}
	return d
}
