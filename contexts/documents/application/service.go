// Package application holds the Documents queries (with permissions and company scope) and the
// subscriptions that keep the register.
package application

import (
	"context"
	"fmt"
	"slices"

	"github.com/jhermoso/karpo-fw-go/contexts/documents/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/documents/domain"
	app "github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/application/orchestration"
	"github.com/jhermoso/karpo-fw-go/pkg/application/pipeline"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// MaxTrail bounds the documents of a trail.
const MaxTrail = 200

// Deps are the ports the use cases need; Audit is optional.
type Deps struct {
	Documents domain.DocumentRepository
	UoW       fw.UnitOfWork
	Audit     app.AuditLog
}

// Service exposes the queries.
type Service struct {
	Get    app.QueryHandler[GetDocument, DocumentDTO]
	Search app.QueryHandler[SearchDocuments, fw.Page[DocumentDTO]]
	Trail  app.QueryHandler[GetTrail, []DocumentDTO]
}

type service struct {
	Deps
	docs *orchestration.Orchestrator[domain.DocumentID, *domain.Document]
}

func newService(d Deps) service {
	var opts []orchestration.Option
	if d.Audit != nil {
		opts = append(opts, orchestration.WithAuditLog(d.Audit))
	}
	return service{Deps: d, docs: orchestration.New[domain.DocumentID, *domain.Document](d.Documents, d.UoW, opts...)}
}

type scope struct {
	global bool
	orgs   []domain.OrganizationID
}

func scopeOf(ctx context.Context) scope {
	ac, ok := authz.FromContext(ctx)
	if !ok {
		return scope{}
	}
	s := scope{global: ac.GlobalAdmin}
	for _, u := range ac.EffectiveOrganizations {
		s.orgs = append(s.orgs, domain.OrganizationID{UUID: u})
	}
	return s
}

// check returns a uniform 404 outside the company's scope.
func (s scope) check(id fmt.Stringer, org domain.OrganizationID) error {
	if !s.global && !slices.Contains(s.orgs, org) {
		return fw.NotFound(domain.DocumentKind, id)
	}
	return nil
}

func (s scope) within() spec.Spec[*domain.Document] {
	switch {
	case s.global:
		return spec.All[*domain.Document]()
	case len(s.orgs) == 0:
		return spec.None[*domain.Document]()
	}
	return domain.DocFieldCompany.In(s.orgs...)
}

func parseID(v *fw.Validation, field, s string) fw.UUID {
	u, err := fw.ParseUUID(s)
	v.Require(err == nil && !u.IsZero(), field, "format", field+" must be an id")
	return u
}

// GetDocument loads an entry by its identity, or by the fact it stands for.
type GetDocument struct {
	ID           domain.DocumentID
	Type, FactID string
}

// SearchDocuments searches the register of the caller's scope.
type SearchDocuments struct {
	Company, Type, Party, Number, From, To string
	Live                                   bool // not cancelled
	Page, Size                             int
}

// GetTrail returns the whole trail a document belongs to: from the first document it comes from
// down to everything that came from it, oldest first.
type GetTrail struct{ ID domain.DocumentID }

// DocumentDTO is the transport form of an entry.
type DocumentDTO struct {
	ID           string `json:"id"`
	Company      string `json:"company"`
	Type         string `json:"type"`
	FactID       string `json:"factId"`
	Number       string `json:"number"`
	Reference    string `json:"reference,omitempty"`
	Date         string `json:"date"`
	Party        string `json:"party,omitempty"`
	Total        string `json:"total,omitempty"`
	OriginType   string `json:"originType,omitempty"`
	OriginID     string `json:"originId,omitempty"`
	Relation     string `json:"relation,omitempty"`
	Cancelled    bool   `json:"cancelled"`
	CancelReason string `json:"cancelReason,omitempty"`
	Version      int64  `json:"version"`
}

func documentDTO(d *domain.Document) DocumentDTO {
	s := d.State()
	out := DocumentDTO{ID: d.ID().String(), Company: s.Company.String(), Type: string(s.Fact.Type), FactID: s.Fact.ID, Number: s.Number,
		Reference: s.Reference, Date: s.Date.String(), OriginType: string(s.Origin.Type), OriginID: s.Origin.ID, Relation: s.Relation,
		Cancelled: s.Cancelled, CancelReason: s.CancelReason, Version: d.Version()}
	if !s.Party.IsZero() {
		out.Party = s.Party.String()
	}
	if s.HasTotal {
		out.Total = s.Total.StringFixed(2)
	}
	return out
}

func (s service) byFact(ctx context.Context, r domain.Ref) (*domain.Document, error) {
	found, err := s.Documents.Find(ctx, domain.ByFact(r))
	if err != nil || len(found) == 0 {
		return nil, err
	}
	return found[0], nil
}

// trail walks up to the first document and then down through everything derived from it.
func (s service) trail(ctx context.Context, d *domain.Document) ([]*domain.Document, error) {
	root, seen := d, map[domain.Ref]bool{d.State().Fact: true}
	for !root.State().Origin.IsZero() && len(seen) < MaxTrail {
		up, err := s.byFact(ctx, root.State().Origin)
		if err != nil {
			return nil, err
		}
		if up == nil || seen[up.State().Fact] {
			break // the origin is not in the register (yet), or the data loops
		}
		root, seen[up.State().Fact] = up, true
	}
	out, queue := []*domain.Document{root}, []*domain.Document{root}
	visited := map[domain.Ref]bool{root.State().Fact: true}
	for len(queue) > 0 && len(out) < MaxTrail {
		derived, err := s.Documents.Find(ctx, domain.DerivedFrom(queue[0].State().Fact), domain.DocFieldDate.Asc())
		if err != nil {
			return nil, err
		}
		queue = queue[1:]
		for _, x := range derived {
			if !visited[x.State().Fact] {
				visited[x.State().Fact] = true
				out, queue = append(out, x), append(queue, x)
			}
		}
	}
	slices.SortStableFunc(out, func(a, b *domain.Document) int { return a.State().Date.Compare(b.State().Date) })
	return out, nil
}

func guard[In, Out any](fn func(context.Context, In) (Out, error)) app.Handler[In, Out] {
	return app.Chain[In, Out](app.HandlerFunc[In, Out](fn), pipeline.RequirePermission[In, Out](PermDocumentRead))
}

// NewService wires the queries.
func NewService(d Deps) *Service {
	s := newService(d)
	svc := &Service{}

	svc.Get = guard(func(ctx context.Context, q GetDocument) (DocumentDTO, error) {
		var doc *domain.Document
		var err error
		if q.ID.IsZero() {
			var v fw.Validation
			v.Require(slices.Contains(domain.Types, domain.Type(q.Type)), "type", "enum", "a document type")
			v.Require(q.FactID != "", "factId", "required", "the identity of the fact is required")
			if err := v.Err(); err != nil {
				return DocumentDTO{}, err
			}
			ref := domain.Ref{Type: domain.Type(q.Type), ID: q.FactID}
			if doc, err = s.byFact(ctx, ref); err == nil && doc == nil {
				err = fw.NotFound(domain.DocumentKind, fw.UUID{})
			}
		} else {
			doc, err = d.Documents.Get(ctx, q.ID)
		}
		if err != nil {
			return DocumentDTO{}, err
		}
		if err := scopeOf(ctx).check(doc.ID(), doc.State().Company); err != nil {
			return DocumentDTO{}, err
		}
		return documentDTO(doc), nil
	})

	svc.Search = guard(func(ctx context.Context, q SearchDocuments) (fw.Page[DocumentDTO], error) {
		var v fw.Validation
		parts := []spec.Specification[*domain.Document]{scopeOf(ctx).within()}
		if q.Company != "" {
			parts = append(parts, domain.DocFieldCompany.Eq(domain.OrganizationID{UUID: parseID(&v, "company", q.Company)}))
		}
		if q.Type != "" {
			v.Require(slices.Contains(domain.Types, domain.Type(q.Type)), "type", "enum", "a document type")
			parts = append(parts, domain.DocFieldType.Eq(q.Type))
		}
		if q.Party != "" {
			parts = append(parts, domain.DocFieldParty.Eq(domain.PartyID{UUID: parseID(&v, "party", q.Party)}))
		}
		if q.Number != "" {
			parts = append(parts, domain.DocFieldNumber.ContainsFold(q.Number))
		}
		if q.From != "" {
			day, err := vocab.ParseDate(q.From)
			v.Require(err == nil, "from", "format", "from must be a date")
			parts = append(parts, domain.DocFieldDate.Ge(day))
		}
		if q.To != "" {
			day, err := vocab.ParseDate(q.To)
			v.Require(err == nil, "to", "format", "to must be a date")
			parts = append(parts, domain.DocFieldDate.Le(day))
		}
		if q.Live {
			parts = append(parts, domain.DocFieldCancelled.Eq(false))
		}
		if err := v.Err(); err != nil {
			return fw.Page[DocumentDTO]{}, err
		}
		page, err := d.Documents.FindPage(ctx, spec.And(parts...), fw.NewPageRequest(q.Page, q.Size, domain.DocFieldDate.Asc(), domain.DocFieldNumber.Asc()))
		if err != nil {
			return fw.Page[DocumentDTO]{}, err
		}
		return fw.MapPage(page, documentDTO), nil
	})

	svc.Trail = guard(func(ctx context.Context, q GetTrail) ([]DocumentDTO, error) {
		doc, err := d.Documents.Get(ctx, q.ID)
		if err != nil {
			return nil, err
		}
		if err := scopeOf(ctx).check(doc.ID(), doc.State().Company); err != nil {
			return nil, err
		}
		docs, err := s.trail(ctx, doc)
		if err != nil {
			return nil, err
		}
		out := []DocumentDTO{}
		for _, x := range docs {
			if x.State().Company == doc.State().Company {
				out = append(out, documentDTO(x))
			}
		}
		return out, nil
	})
	return svc
}

// Register implements contracts.Register on the repository: the port other contexts use.
type Register struct{ Documents domain.DocumentRepository }

var _ contracts.Register = Register{}

// ByFact implements contracts.Register.
func (r Register) ByFact(ctx context.Context, docType, factID string) (contracts.Document, bool, error) {
	found, err := r.Documents.Find(ctx, domain.ByFact(domain.Ref{Type: domain.Type(docType), ID: factID}))
	if err != nil || len(found) == 0 {
		return contracts.Document{}, false, err
	}
	s := found[0].State()
	return contracts.Document{ID: found[0].ID().String(), Company: s.Company.String(), Type: string(s.Fact.Type), FactID: s.Fact.ID, Number: s.Number,
		Date: s.Date.String(), Cancelled: s.Cancelled}, true, nil
}
