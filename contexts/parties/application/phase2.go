package application

import (
	"context"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/application/pipeline"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// AddIdentification adds an identity document to a party.
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

// RemoveIdentification removes an identity document.
type RemoveIdentification struct {
	PartyID domain.PartyID
	ID      domain.IdentificationID
}

// AddressDTO is the transport representation of a postal address.
type AddressDTO struct {
	StreetType    string `json:"streetType,omitempty"`
	Line1         string `json:"line1"`
	Line2         string `json:"line2,omitempty"`
	Directions    string `json:"directions,omitempty"`
	PostalCode    string `json:"postalCode,omitempty"`
	Locality      string `json:"locality,omitempty"`
	Region        string `json:"region,omitempty"`
	Country       string `json:"country"`
	GeoPostalCode string `json:"geoPostalCode,omitempty"` // Geography context ids
	GeoBoundary   string `json:"geoBoundary,omitempty"`
}

// AddContact adds a contact to a party.
type AddContact struct {
	PartyID         domain.PartyID `json:"-"`
	Kind            string         `json:"kind"`
	Value           string         `json:"value,omitempty"`
	Address         *AddressDTO    `json:"address,omitempty"`
	Purposes        []string       `json:"purposes,omitempty"`
	NonSolicitation bool           `json:"nonSolicitation,omitempty"`
	From            *time.Time     `json:"from,omitempty"`
}

// SetContactPurposes replaces the purposes of a contact.
type SetContactPurposes struct {
	PartyID   domain.PartyID   `json:"-"`
	ContactID domain.ContactID `json:"-"`
	Purposes  []string         `json:"purposes"`
}

// EndContact stops using a contact (now when At is nil).
type EndContact struct {
	PartyID   domain.PartyID   `json:"-"`
	ContactID domain.ContactID `json:"-"`
	At        *time.Time       `json:"at,omitempty"`
}

// Classify classifies a party from a moment on (now when From is nil).
type Classify struct {
	PartyID        domain.PartyID `json:"-"`
	Classification string         `json:"classification"`
	From           *time.Time     `json:"from,omitempty"`
}

// EndClassification ends a classification (now when At is nil).
type EndClassification struct {
	PartyID domain.PartyID          `json:"-"`
	ID      domain.ClassificationID `json:"-"`
	At      *time.Time              `json:"at,omitempty"`
}

// DocumentOptions lists the document types available in a country.
type DocumentOptions struct{ Country string }

// ListClassificationTypes lists the classification catalog.
type ListClassificationTypes struct{}

// DocumentOptionDTO is a document type available in a country.
type DocumentOptionDTO struct {
	DocumentType             string `json:"documentType"`
	Code                     string `json:"code"`
	Name                     string `json:"name"`
	Default                  bool   `json:"default"`
	Pattern                  string `json:"pattern,omitempty"`
	RequiresExpiry           bool   `json:"requiresExpiry"`
	RequiresIssuingAuthority bool   `json:"requiresIssuingAuthority"`
}

// ClassificationTypeDTO is an entry of the classification catalog.
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

// addPhase2 wires identifications, contacts and classifications.
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
		// A document identifies one party: the C# only offered a lookup by value.
		// Every party counts, visible or not: a document identifies one party in the whole
		// installation (decision P1: the base identity is shared).
		taken, err := s.Parties.Exists(ctx, spec.And(domain.HoldsDocument(d.Type, d.Country.String(), number), spec.Not(domain.WithIDs(c.PartyID))))
		if err != nil {
			return PartyDTO{}, err
		}
		if taken {
			return PartyDTO{}, fw.Violation("parties.document_taken", "another party holds this document")
		}
		return updateParty(ctx, c.PartyID, func(p *domain.Party, _ *domain.Catalog) error {
			_, err := p.AddIdentification(policy, d, c.Primary)
			return err
		})
	}, pipeline.Transactional[AddIdentification, PartyDTO](s.UoW))

	svc.RemoveIdentification = chain(PermPartyUpdate, func(ctx context.Context, c RemoveIdentification) (PartyDTO, error) {
		return updateParty(ctx, c.PartyID, func(p *domain.Party, _ *domain.Catalog) error { return p.RemoveIdentification(c.ID) })
	}, pipeline.RetryOnConflict[RemoveIdentification, PartyDTO](retry, backoff))

	svc.AddContact = chain(PermPartyUpdate, func(ctx context.Context, c AddContact) (PartyDTO, error) {
		d, err := c.data()
		if err != nil {
			return PartyDTO{}, err
		}
		if d.Kind == domain.ContactPostal && s.Addresses != nil {
			if d.Address, err = s.Addresses.CheckAddress(ctx, d.Address); err != nil {
				return PartyDTO{}, err
			}
		}
		return updateParty(ctx, c.PartyID, func(p *domain.Party, _ *domain.Catalog) error {
			_, err := p.AddContact(d, nowOr(c.From))
			return err
		})
	}, pipeline.RetryOnConflict[AddContact, PartyDTO](retry, backoff))

	svc.SetContactPurposes = chain(PermPartyUpdate, func(ctx context.Context, c SetContactPurposes) (PartyDTO, error) {
		ps, err := parsePurposes(c.Purposes)
		if err != nil {
			return PartyDTO{}, err
		}
		return updateParty(ctx, c.PartyID, func(p *domain.Party, _ *domain.Catalog) error { return p.SetContactPurposes(c.ContactID, ps) })
	}, pipeline.RetryOnConflict[SetContactPurposes, PartyDTO](retry, backoff))

	svc.EndContact = chain(PermPartyUpdate, func(ctx context.Context, c EndContact) (PartyDTO, error) {
		return updateParty(ctx, c.PartyID, func(p *domain.Party, _ *domain.Catalog) error { return p.EndContact(c.ContactID, nowOr(c.At)) })
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
