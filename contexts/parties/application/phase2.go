package application

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/application/pipeline"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// AddIdentification añade un documento de identidad a una party.
type AddIdentification struct {
	PartyID          domain.PartyID `json:"-"`
	DocumentType     string         `json:"documentType"`
	Country          string         `json:"country"`
	Number           string         `json:"number"`
	IssuingAuthority string         `json:"issuingAuthority,omitempty"`
	IssuedOn         string         `json:"issuedOn,omitempty"`  // YYYY-MM-DD
	ExpiresOn        string         `json:"expiresOn,omitempty"` // YYYY-MM-DD
	Primary          bool           `json:"primary,omitempty"`
}

// RemoveIdentification quita un documento de identidad.
type RemoveIdentification struct {
	PartyID domain.PartyID
	ID      domain.IdentificationID
}

// AddressDTO es la representación de transporte de una dirección postal.
type AddressDTO struct {
	StreetType string `json:"streetType,omitempty"`
	Line1      string `json:"line1"`
	// Dónde en la calle, pieza a pieza (los formularios españoles las piden una a una). Todas
	// opcionales.
	Number          string `json:"number,omitempty"`
	NumberQualifier string `json:"numberQualifier,omitempty"` // bis, duplicado
	Block           string `json:"block,omitempty"`
	Portal          string `json:"portal,omitempty"`
	Staircase       string `json:"staircase,omitempty"`
	Floor           string `json:"floor,omitempty"`
	Door            string `json:"door,omitempty"`
	Line2           string `json:"line2,omitempty"`
	Directions      string `json:"directions,omitempty"`
	PostalCode      string `json:"postalCode,omitempty"`
	Locality        string `json:"locality,omitempty"`
	Region          string `json:"region,omitempty"`
	Country         string `json:"country"`
	GeoPostalCode   string `json:"geoPostalCode,omitempty"` // ids del contexto Geography
	GeoBoundary     string `json:"geoBoundary,omitempty"`
}

// AddContact añade un contacto a una party.
type AddContact struct {
	PartyID         domain.PartyID `json:"-"`
	Kind            string         `json:"kind"`
	Value           string         `json:"value,omitempty"`
	Address         *AddressDTO    `json:"address,omitempty"`
	Purposes        []string       `json:"purposes,omitempty"`
	NonSolicitation bool           `json:"nonSolicitation,omitempty"`
	From            *time.Time     `json:"from,omitempty"`
	// Owner es la organización interna dueña del contacto (D-122): sólo ella lo ve y lo cambia.
	// Vacío: de todos si quien llama puede cambiar la party; si no, la única organización en la
	// que escribe, y si escribe en varias, hay que decirla.
	Owner string `json:"owner,omitempty"`
}

// SetContactPurposes sustituye los usos de un contacto.
type SetContactPurposes struct {
	PartyID   domain.PartyID   `json:"-"`
	ContactID domain.ContactID `json:"-"`
	Purposes  []string         `json:"purposes"`
}

// EndContact deja de usar un contacto (ahora si At es nil).
type EndContact struct {
	PartyID   domain.PartyID   `json:"-"`
	ContactID domain.ContactID `json:"-"`
	At        *time.Time       `json:"at,omitempty"`
}

// Classify clasifica una party desde un momento (ahora si From es nil).
type Classify struct {
	PartyID        domain.PartyID `json:"-"`
	Classification string         `json:"classification"`
	From           *time.Time     `json:"from,omitempty"`
}

// EndClassification termina una clasificación (ahora si At es nil).
type EndClassification struct {
	PartyID domain.PartyID          `json:"-"`
	ID      domain.ClassificationID `json:"-"`
	At      *time.Time              `json:"at,omitempty"`
}

// DocumentOptions lista los tipos de documento disponibles en un país.
type DocumentOptions struct{ Country string }

// ListClassificationTypes lista el catálogo de clasificaciones.
type ListClassificationTypes struct{}

// DocumentOptionDTO es un tipo de documento disponible en un país.
type DocumentOptionDTO struct {
	DocumentType             string `json:"documentType"`
	Code                     string `json:"code"`
	Name                     string `json:"name"`
	Default                  bool   `json:"default"`
	Pattern                  string `json:"pattern,omitempty"`
	RequiresExpiry           bool   `json:"requiresExpiry"`
	RequiresIssuingAuthority bool   `json:"requiresIssuingAuthority"`
}

// ClassificationTypeDTO es una entrada del catálogo de clasificaciones.
type ClassificationTypeDTO struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Family      string `json:"family,omitempty"`
	Active      bool   `json:"active"`
	AppliesTo   string `json:"appliesTo"`
	Exclusive   bool   `json:"exclusive"`
}

func (s service) documentPolicy(ctx context.Context) (*domain.DocumentPolicy, error) {
	types, err := s.Catalogs.DocumentTypes(ctx)
	if err != nil {
		return nil, err
	}
	rules, err := s.Catalogs.CountryDocumentRules(ctx)
	if err != nil {
		return nil, err
	}
	return domain.NewDocumentPolicy(types, rules)
}

func (s service) classificationCatalog(ctx context.Context) (*domain.ClassificationCatalog, error) {
	types, err := s.Catalogs.ClassificationTypes(ctx)
	if err != nil {
		return nil, err
	}
	return domain.NewClassificationCatalog(types)
}

func (c AddIdentification) data() (domain.IdentificationData, error) {
	var v fw.Validation
	t, err := domain.ParseDocumentTypeID(c.DocumentType)
	v.Require(err == nil, "documentType", "format", "document type must be a document type id")
	country, err := vocab.NewCountryCode(c.Country)
	v.Merge("country", err)
	date := func(field, s string) vocab.Date {
		if s == "" {
			return vocab.Date{}
		}
		d, err := vocab.ParseDate(s)
		v.Require(err == nil, field, "format", field+" must be YYYY-MM-DD")
		return d
	}
	d := domain.IdentificationData{Type: t, Country: country, Number: c.Number, IssuingAuthority: c.IssuingAuthority,
		IssuedOn: date("issuedOn", c.IssuedOn), ExpiresOn: date("expiresOn", c.ExpiresOn)}
	return d, v.Err()
}

func (c AddContact) data() (domain.ContactData, error) {
	var v fw.Validation
	d := domain.ContactData{Kind: domain.ContactKind(c.Kind), Value: c.Value, NonSolicitation: c.NonSolicitation}
	for _, p := range c.Purposes {
		pp, err := domain.ParsePurpose(p)
		v.Merge("", err)
		d.Purposes = append(d.Purposes, pp)
	}
	if d.Kind == domain.ContactPostal {
		if c.Address == nil {
			v.Add("address", "required", "a postal contact needs an address")
			return d, v.Err()
		}
		a := c.Address
		country, err := vocab.NewCountryCode(a.Country)
		v.Merge("address.country", err)
		geo := func(field, s string) fw.UUID {
			if s == "" {
				return fw.UUID{}
			}
			u, err := fw.ParseUUID(s)
			v.Require(err == nil, field, "format", field+" must be a geography id")
			return u
		}
		d.Address = domain.PostalAddress{StreetType: a.StreetType, Line1: a.Line1, Line2: a.Line2, Directions: a.Directions,
			Premises: domain.Premises{Number: a.Number, Qualifier: a.NumberQualifier, Block: a.Block, Portal: a.Portal,
				Staircase: a.Staircase, Floor: a.Floor, Door: a.Door},
			PostalCode: a.PostalCode, Locality: a.Locality, Region: a.Region, Country: country,
			Geo: domain.GeoRef{PostalCode: geo("address.geoPostalCode", a.GeoPostalCode), Boundary: geo("address.geoBoundary", a.GeoBoundary)}}
	}
	return d, v.Err()
}

func parsePurposes(ps []string) ([]domain.Purpose, error) {
	var v fw.Validation
	out := make([]domain.Purpose, 0, len(ps))
	for _, p := range ps {
		pp, err := domain.ParsePurpose(p)
		v.Merge("", err)
		out = append(out, pp)
	}
	return out, v.Err()
}

// addPhase2 conecta identificaciones, contactos y clasificaciones.
func addPhase2(svc *Service, s service) {
	updateParty := s.updateParty
	retry, backoff := 3, 10*time.Millisecond

	svc.AddIdentification = chain(PermPartyUpdate, func(ctx context.Context, c AddIdentification) (PartyDTO, error) {
		d, err := c.data()
		if err != nil {
			return PartyDTO{}, err
		}
		policy, err := s.documentPolicy(ctx)
		if err != nil {
			return PartyDTO{}, err
		}
		number, err := policy.Validate(d)
		if err != nil {
			return PartyDTO{}, err
		}
		// La party se mira antes de buscar el documento en las demás: a quien no puede cambiarla no
		// se le dice si el documento está cogido, y a quien pone un CIF a una persona se le dice
		// eso, no que el CIF es de otra party (D-85). updateParty lo vuelve a comprobar al cambiarla.
		sc := s.scope(ctx)
		p, err := sc.visible(s.Parties.Get(ctx, c.PartyID))
		if err != nil {
			return PartyDTO{}, err
		}
		if err := sc.writable(p); err != nil {
			return PartyDTO{}, err
		}
		if err := operable(p); err != nil {
			return PartyDTO{}, err
		}
		// Añadir un documento a una party que ya tiene alguno es corregir su identidad (D-122, regla
		// 3); darle el primero es completarla, y lo hace quien puede cambiarla (decidido en esta
		// sesión: el alta no lleva documento, y si no, el usuario estándar no podría identificar a los
		// clientes que da de alta).
		if len(p.Identifications()) > 0 {
			if err := sc.mayCorrectIdentity(p); err != nil {
				return PartyDTO{}, err
			}
		}
		if err := policy.CheckKind(p.Kind(), d); err != nil {
			return PartyDTO{}, err
		}
		// Un documento identifica a una sola party: el C# sólo ofrecía una búsqueda por valor.
		// Cuenta toda party, visible o no: un documento identifica a una sola party en toda la
		// instalación (decisión P1: la identidad base es compartida).
		taken, err := s.Parties.Exists(ctx, spec.And(domain.HoldsDocument(d.Type, d.Country.String(), number), spec.Not(domain.WithIDs(c.PartyID))))
		if err != nil {
			return PartyDTO{}, err
		}
		if taken {
			return PartyDTO{}, fw.Violation("parties.document_taken", "another party holds this document")
		}
		return updateParty(ctx, c.PartyID, func(p *domain.Party, _ *domain.Catalog) error {
			if len(p.Identifications()) > 0 {
				if err := s.scope(ctx).mayCorrectIdentity(p); err != nil {
					return err
				}
			}
			_, err := p.AddIdentification(policy, d, c.Primary)
			return err
		})
	}, pipeline.Transactional[AddIdentification, PartyDTO](s.UoW))

	svc.RemoveIdentification = chain(PermPartyUpdate, func(ctx context.Context, c RemoveIdentification) (PartyDTO, error) {
		return updateParty(ctx, c.PartyID, func(p *domain.Party, _ *domain.Catalog) error {
			if err := s.scope(ctx).mayCorrectIdentity(p); err != nil { // D-122, regla 3
				return err
			}
			return p.RemoveIdentification(c.ID)
		})
	}, pipeline.RetryOnConflict[RemoveIdentification, PartyDTO](retry, backoff))

	// Un e-mail o un teléfono con formato erróneo se guarda: la respuesta lo avisa en warnings, con
	// el campo del cuerpo (value), y la ficha queda con la incidencia (D-92). Un contacto con dueña
	// (D-122) lo añade quien escribe en la dueña, aunque no pueda cambiar la party: es de su perfil.
	svc.AddContact = chain(PermPartyUpdate, func(ctx context.Context, c AddContact) (ContactResult, error) {
		d, err := c.data()
		if err != nil {
			return ContactResult{}, err
		}
		sc := s.scope(ctx)
		p, err := sc.visible(s.Parties.Get(ctx, c.PartyID))
		if err != nil {
			return ContactResult{}, err
		}
		if d.Owner, err = s.contactOwner(ctx, sc, p, c.Owner); err != nil {
			return ContactResult{}, err
		}
		if d.Kind == domain.ContactPostal && s.Addresses != nil {
			if d.Address, err = s.Addresses.CheckAddress(ctx, d.Address); err != nil {
				return ContactResult{}, err
			}
		}
		warnings := []WarningDTO{}
		dto, err := s.changeParty(ctx, c.PartyID, func(sc scope, p *domain.Party) error {
			if !sc.sees(p, fw.Now()) {
				return fw.NotFound(domain.PartyKind, p.ID())
			}
			if d.Owner.IsZero() {
				return sc.writable(p)
			}
			if !sc.canWriteOrg(d.Owner) {
				return fmt.Errorf("%w: a contact of an organization is added by who writes in it", fw.ErrForbidden)
			}
			return ownedContactAllowed(sc, p)
		}, func(p *domain.Party, _ *domain.Catalog) error {
			id, err := p.AddContact(d, nowOr(c.From))
			if err != nil {
				return err
			}
			warnings = []WarningDTO{} // otro intento tras un conflicto empieza de cero
			for _, i := range p.DataQualityIssuesOf(id.UUID) {
				warnings = append(warnings, WarningDTO{Field: "value", Code: i.Code, Message: i.Message})
			}
			return nil
		})
		if err != nil {
			return ContactResult{}, err
		}
		return ContactResult{PartyDTO: dto, Warnings: warnings}, nil
	}, pipeline.RetryOnConflict[AddContact, ContactResult](retry, backoff))

	svc.SetContactPurposes = chain(PermPartyUpdate, func(ctx context.Context, c SetContactPurposes) (PartyDTO, error) {
		ps, err := parsePurposes(c.Purposes)
		if err != nil {
			return PartyDTO{}, err
		}
		return s.changeParty(ctx, c.PartyID, contactWritable(c.ContactID), func(p *domain.Party, _ *domain.Catalog) error {
			return p.SetContactPurposes(c.ContactID, ps)
		})
	}, pipeline.RetryOnConflict[SetContactPurposes, PartyDTO](retry, backoff))

	svc.EndContact = chain(PermPartyUpdate, func(ctx context.Context, c EndContact) (PartyDTO, error) {
		return s.changeParty(ctx, c.PartyID, contactWritable(c.ContactID), func(p *domain.Party, _ *domain.Catalog) error {
			return p.EndContact(c.ContactID, nowOr(c.At))
		})
	}, pipeline.RetryOnConflict[EndContact, PartyDTO](retry, backoff))

	svc.Classify = chain(PermPartyUpdate, func(ctx context.Context, c Classify) (PartyDTO, error) {
		typ, err := domain.ParseClassificationTypeID(c.Classification)
		if err != nil {
			var v fw.Validation
			v.Add("classification", "format", "classification must be a classification id")
			return PartyDTO{}, v.Err()
		}
		cc, err := s.classificationCatalog(ctx)
		if err != nil {
			return PartyDTO{}, err
		}
		return updateParty(ctx, c.PartyID, func(p *domain.Party, _ *domain.Catalog) error {
			_, err := p.Classify(cc, typ, nowOr(c.From))
			return err
		})
	}, pipeline.RetryOnConflict[Classify, PartyDTO](retry, backoff))

	svc.EndClassification = chain(PermPartyUpdate, func(ctx context.Context, c EndClassification) (PartyDTO, error) {
		return updateParty(ctx, c.PartyID, func(p *domain.Party, _ *domain.Catalog) error { return p.EndClassification(c.ID, nowOr(c.At)) })
	}, pipeline.RetryOnConflict[EndClassification, PartyDTO](retry, backoff))

	svc.DocumentOptions = chain(PermPartyRead, func(ctx context.Context, q DocumentOptions) ([]DocumentOptionDTO, error) {
		country, err := vocab.NewCountryCode(q.Country)
		if err != nil {
			return nil, err
		}
		policy, err := s.documentPolicy(ctx)
		if err != nil {
			return nil, err
		}
		out := []DocumentOptionDTO{}
		for _, r := range policy.Options(country) {
			t, _ := policy.DocumentType(r.DocumentType)
			o := DocumentOptionDTO{DocumentType: t.ID.String(), Code: t.Code, Name: t.Name.String(), Default: r.Default, Pattern: r.Pattern,
				RequiresExpiry: t.RequiresExpiry, RequiresIssuingAuthority: t.RequiresIssuingAuthority}
			if r.RequiresExpiry != nil {
				o.RequiresExpiry = *r.RequiresExpiry
			}
			if r.RequiresIssuingAuthority != nil {
				o.RequiresIssuingAuthority = *r.RequiresIssuingAuthority
			}
			out = append(out, o)
		}
		return out, nil
	})

	svc.ListClassificationTypes = chain(PermPartyRead, func(ctx context.Context, _ ListClassificationTypes) ([]ClassificationTypeDTO, error) {
		cc, err := s.classificationCatalog(ctx)
		if err != nil {
			return nil, err
		}
		out := []ClassificationTypeDTO{}
		for _, t := range cc.All() {
			d := ClassificationTypeDTO{ID: t.ID.String(), Name: t.Name.String(), Description: t.Description, Active: t.Active,
				AppliesTo: string(t.AppliesTo), Exclusive: t.Exclusive}
			if t.Family != nil {
				d.Family = t.Family.String()
			}
			out = append(out, d)
		}
		return out, nil
	})
}

// contactWritable comprueba que el llamante puede cambiar un contacto (sus propósitos, su fin). Uno
// con dueña (D-122) sólo existe para quien ve a la dueña (si no, 404, como si no estuviera) y lo
// cambia quien escribe en ella, pueda o no cambiar la party; uno de todos, quien puede cambiar la
// party.
func contactWritable(id domain.ContactID) func(scope, *domain.Party) error {
	return func(sc scope, p *domain.Party) error {
		if !sc.sees(p, fw.Now()) {
			return fw.NotFound(domain.PartyKind, p.ID())
		}
		c, ok := p.Contact(id)
		if !ok || !c.IsOwned() {
			return sc.writable(p)
		}
		if !sc.seesOwned(c.Owner) {
			return fw.NotFound("parties.contact", id)
		}
		if !sc.canWriteOrg(c.Owner) {
			return fmt.Errorf("%w: a contact of an organization is changed by who writes in it", fw.ErrForbidden)
		}
		return nil
	}
}

// contactOwner decide la dueña de un contacto nuevo (D-122). Pedida: una organización interna en la
// que el llamante escribe. Sin pedir: ninguna (de todos) para el administrador global y para quien
// puede cambiar la party; para quien sólo se relaciona con ella, la única organización en la que
// escribe y a la que la party está afiliada (si ninguna lo está, la única en la que escribe); si hay
// varias, tiene que decirla (decidido en esta sesión).
func (s service) contactOwner(ctx context.Context, sc scope, p *domain.Party, requested string) (domain.PartyID, error) {
	if requested == "" {
		if sc.global || sc.canWrite(p, fw.Now()) {
			return domain.PartyID{}, nil
		}
		orgs := sc.writableOrgs()
		if len(orgs) == 0 {
			return domain.PartyID{}, sc.writable(p) // ni la party ni una organización: el 403 de siempre
		}
		// Entre las suyas, las que se relacionan con la party; si ninguna, todas las suyas.
		related := slices.DeleteFunc(slices.Clone(orgs), func(o domain.PartyID) bool { return !slices.Contains(p.OrganizationsAt(fw.Now()), o) })
		switch {
		case len(related) == 1:
			return s.checkOwner(ctx, sc, p, related[0])
		case len(related) == 0 && len(orgs) == 1:
			return s.checkOwner(ctx, sc, p, orgs[0])
		}
		var v fw.Validation
		v.Add("owner", "required", "say which of your organizations owns the contact: you cannot change the party itself")
		return domain.PartyID{}, v.Err()
	}
	owner, err := domain.ParsePartyID(requested)
	if err != nil || owner.IsZero() {
		var v fw.Validation
		v.Add("owner", "format", "owner must be the id of an internal organization")
		return domain.PartyID{}, v.Err()
	}
	return s.checkOwner(ctx, sc, p, owner)
}

// checkOwner comprueba la dueña de un contacto nuevo: una organización interna en la que el
// llamante escribe, y una party que admite contactos de una empresa (ownedContactAllowed).
func (s service) checkOwner(ctx context.Context, sc scope, p *domain.Party, owner domain.PartyID) (domain.PartyID, error) {
	if !sc.canWriteOrg(owner) {
		return domain.PartyID{}, fmt.Errorf("%w: a contact is owned by an organization you write in", fw.ErrForbidden)
	}
	if err := ownedContactAllowed(sc, p); err != nil {
		return domain.PartyID{}, err
	}
	org, err := s.Parties.Get(ctx, owner)
	if err != nil {
		if errors.Is(err, fw.ErrNotFound) {
			return domain.PartyID{}, fw.Violation("parties.contact_owner_not_internal", "the owner of a contact is an internal organization")
		}
		return domain.PartyID{}, err
	}
	if !isInternalOrganization(org, fw.Now()) {
		return domain.PartyID{}, fw.Violation("parties.contact_owner_not_internal", "the owner of a contact is an internal organization")
	}
	return owner, nil
}

// ownedContactAllowed se niega a que una empresa añada un contacto suyo a una organización interna
// de otros o a una entrada compartida del catálogo: su ficha la mantienen sus propios usuarios o el
// administrador global (revisión de D-122). El global, sí.
func ownedContactAllowed(sc scope, p *domain.Party) error {
	if sc.global {
		return nil
	}
	if p.IsShared() {
		return fmt.Errorf("%w: a shared entry of the catalog takes no contacts of a company", fw.ErrForbidden)
	}
	if isInternalOrganization(p, fw.Now()) && !sc.canWriteOrg(p.ID()) {
		return fmt.Errorf("%w: an internal organization of others takes no contacts of your company", fw.ErrForbidden)
	}
	return nil
}
