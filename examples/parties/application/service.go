// Package application holds the Parties use cases: typed commands and queries, their handlers
// decorated with the framework middleware, and the DTOs exposed to the transport layer.
package application

import (
	"context"
	"time"

	"github.com/jhermoso/karpo-fw-go/examples/parties/domain"
	app "github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/orchestration"
	"github.com/jhermoso/karpo-fw-go/pkg/application/pipeline"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
)

// ContactDTO is the transport representation of a contact.
type ContactDTO struct {
	Kind    string `json:"kind"`
	Value   string `json:"value"`
	Primary bool   `json:"primary"`
}

// PartyDTO is the transport representation of a party.
type PartyDTO struct {
	ID           domain.PartyID `json:"id"`
	Type         string         `json:"type"`
	LegalName    string         `json:"legalName"`
	TaxID        string         `json:"taxId"`
	Active       bool           `json:"active"`
	RegisteredAt time.Time      `json:"registeredAt"`
	Version      int64          `json:"version"`
	Contacts     []ContactDTO   `json:"contacts"`
}

// ToDTO maps the aggregate to its DTO.
func ToDTO(p *domain.Party) PartyDTO {
	contacts := make([]ContactDTO, 0, len(p.Contacts()))
	for _, c := range p.Contacts() {
		contacts = append(contacts, ContactDTO{Kind: string(c.Kind), Value: c.Value, Primary: c.Primary})
	}
	return PartyDTO{
		ID: p.ID(), Type: string(p.Type()), LegalName: p.LegalName(), TaxID: p.TaxID().String(),
		Active: p.Active(), RegisteredAt: p.RegisteredAt(), Version: p.Version(), Contacts: contacts,
	}
}

// RegisterParty registers a new party.
type RegisterParty struct {
	Type      string `json:"type"`
	LegalName string `json:"legalName"`
	TaxID     string `json:"taxId"`
	RequestID string `json:"-"` // Idempotency-Key header
}

// Validate implements application.Validatable.
func (c RegisterParty) Validate() error {
	var v fw.Validation
	v.Require(c.Type == string(domain.Person) || c.Type == string(domain.Organization), "type", "enum", "type must be person or organization")
	v.Require(c.LegalName != "", "legalName", "required", "legal name is required")
	if _, err := domain.NewTaxID(c.TaxID); err != nil {
		v.Merge("", err)
	}
	return v.Err()
}

// IdempotencyKey implements application.IdempotencyKeyed.
func (c RegisterParty) IdempotencyKey() string { return c.RequestID }

// RenameParty changes the legal name.
type RenameParty struct {
	ID        domain.PartyID `json:"-"`
	LegalName string         `json:"legalName"`
}

// AddContact adds a contact mechanism.
type AddContact struct {
	ID      domain.PartyID `json:"-"`
	Kind    string         `json:"kind"`
	Value   string         `json:"value"`
	Primary bool           `json:"primary"`
}

// GetParty loads one party.
type GetParty struct{ ID domain.PartyID }

// SearchParties searches parties; every criterion becomes part of one specification executed by
// the store (filtering, ordering and paging in the database).
type SearchParties struct {
	Text        string
	ActiveOnly  bool
	ReachableBy string
	MinContacts int
	Page, Size  int
}

// Specification builds the search specification.
func (q SearchParties) Specification() spec.Spec[*domain.Party] {
	parts := []spec.Specification[*domain.Party]{}
	if q.Text != "" {
		parts = append(parts, domain.NameContains(q.Text).Or(domain.FieldTaxID.StartsWith(q.Text)))
	}
	if q.ActiveOnly {
		parts = append(parts, domain.Active())
	}
	if q.ReachableBy != "" {
		parts = append(parts, domain.ReachableBy(domain.ContactKind(q.ReachableBy)))
	}
	if q.MinContacts > 0 {
		parts = append(parts, domain.MinContacts(q.MinContacts))
	}
	return spec.And(parts...)
}

// Service exposes the Parties use cases as decorated, statically typed handlers.
type Service struct {
	Register   app.CommandHandler[RegisterParty, PartyDTO]
	Rename     app.CommandHandler[RenameParty, PartyDTO]
	AddContact app.CommandHandler[AddContact, PartyDTO]
	Get        app.QueryHandler[GetParty, PartyDTO]
	Search     app.QueryHandler[SearchParties, fw.Page[PartyDTO]]
}

// NewService wires the use cases. recorder is optional (outbox).
func NewService(repo domain.Repository, uow fw.UnitOfWork, recorder app.EventRecorder, idem app.IdempotencyStore) *Service {
	var opts []orchestration.Option
	if recorder != nil {
		opts = append(opts, orchestration.WithOutbox(recorder))
	}
	orch := orchestration.New[domain.PartyID, *domain.Party](repo, uow, opts...)

	register := app.HandlerFunc[RegisterParty, PartyDTO](func(ctx context.Context, c RegisterParty) (PartyDTO, error) {
		taxID, err := domain.NewTaxID(c.TaxID)
		if err != nil {
			return PartyDTO{}, err
		}
		if exists, err := repo.Exists(ctx, domain.WithTaxID(taxID)); err != nil {
			return PartyDTO{}, err
		} else if exists {
			return PartyDTO{}, fw.Violation("parties.duplicate_tax_id", "a party with this tax id already exists")
		}
		p, err := domain.Register(domain.NewPartyID(), domain.PartyType(c.Type), c.LegalName, taxID)
		if err != nil {
			return PartyDTO{}, err
		}
		if err := orch.Create(ctx, p); err != nil {
			return PartyDTO{}, err
		}
		return ToDTO(p), nil
	})

	rename := app.HandlerFunc[RenameParty, PartyDTO](func(ctx context.Context, c RenameParty) (PartyDTO, error) {
		p, err := orch.Update(ctx, c.ID, func(_ context.Context, p *domain.Party) error { return p.Rename(c.LegalName) })
		if err != nil {
			return PartyDTO{}, err
		}
		return ToDTO(p), nil
	})

	addContact := app.HandlerFunc[AddContact, PartyDTO](func(ctx context.Context, c AddContact) (PartyDTO, error) {
		p, err := orch.Update(ctx, c.ID, func(_ context.Context, p *domain.Party) error {
			return p.AddContact(domain.Contact{Kind: domain.ContactKind(c.Kind), Value: c.Value, Primary: c.Primary})
		})
		if err != nil {
			return PartyDTO{}, err
		}
		return ToDTO(p), nil
	})

	get := app.HandlerFunc[GetParty, PartyDTO](func(ctx context.Context, q GetParty) (PartyDTO, error) {
		p, err := repo.Get(ctx, q.ID)
		if err != nil {
			return PartyDTO{}, err
		}
		return ToDTO(p), nil
	})

	search := app.HandlerFunc[SearchParties, fw.Page[PartyDTO]](func(ctx context.Context, q SearchParties) (fw.Page[PartyDTO], error) {
		page, err := repo.FindPage(ctx, q.Specification(),
			fw.NewPageRequest(q.Page, q.Size, domain.FieldLegalName.Asc()))
		if err != nil {
			return fw.Page[PartyDTO]{}, err
		}
		return fw.MapPage(page, ToDTO), nil
	})

	return &Service{
		Register: app.Chain[RegisterParty, PartyDTO](register,
			pipeline.Idempotent[RegisterParty, PartyDTO](idem, 24*time.Hour),
			pipeline.Validating[RegisterParty, PartyDTO](),
			pipeline.Transactional[RegisterParty, PartyDTO](uow)),
		Rename: app.Chain[RenameParty, PartyDTO](rename,
			pipeline.RetryOnConflict[RenameParty, PartyDTO](3, 10*time.Millisecond)),
		AddContact: app.Chain[AddContact, PartyDTO](addContact,
			pipeline.RetryOnConflict[AddContact, PartyDTO](3, 10*time.Millisecond)),
		Get:    get,
		Search: search,
	}
}
