package application

import (
	"context"
	"errors"

	"github.com/jhermoso/karpo-fw-go/contexts/fiscal/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/application/pipeline"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
)

// GenerateFiling generates (or regenerates, while a draft) a form of an organization for a
// period from the withholdings reported by Payroll.
type GenerateFiling struct {
	Organization string `json:"organization"`
	Form         string `json:"form"`
	Year         int    `json:"year"`
	Period       string `json:"period"` // AEAT code: 01–12, 1T–4T, 0A
}

// SubmitFiling records the submission of a draft with the AEAT reference (CSV).
type SubmitFiling struct {
	ID        domain.FilingID `json:"-"`
	Reference string          `json:"reference,omitempty"`
}

// RevertFiling reverts a submitted filing.
type RevertFiling struct {
	ID     domain.FilingID `json:"-"`
	Reason string          `json:"reason"`
}

// DiscardFiling deletes a draft.
type DiscardFiling struct{ ID domain.FilingID }

// GetFiling loads a filing.
type GetFiling struct{ ID domain.FilingID }

// SearchFilings searches filings of the caller's scope.
type SearchFilings struct {
	Organization, Form, Status string
	Year, Page, Size           int
}

// RecipientDTO is a line of a filing.
type RecipientDTO struct {
	Party       string `json:"party"`
	NIF         string `json:"nif,omitempty"`
	Name        string `json:"name,omitempty"`
	Province    string `json:"province,omitempty"`
	Key         string `json:"key"`
	Payments    int    `json:"payments"`
	Perceptions string `json:"perceptions"`
	Withheld    string `json:"withheld"`
}

// FilingDTO is the transport form of a filing.
type FilingDTO struct {
	ID            string         `json:"id"`
	Declarant     string         `json:"declarant"`
	DeclarantNIF  string         `json:"declarantNif,omitempty"`
	DeclarantName string         `json:"declarantName,omitempty"`
	Form          string         `json:"form"`
	Year          int            `json:"year"`
	Period        string         `json:"period"`
	Status        string         `json:"status"`
	Number        int64          `json:"number,omitempty"`
	Reference     string         `json:"reference,omitempty"`
	Recipients    int            `json:"recipients"`
	Perceptions   string         `json:"perceptions"`
	Withheld      string         `json:"withheld"`
	Lines         []RecipientDTO `json:"lines"`
	Problems      []string       `json:"problems"`
	RevertReason  string         `json:"revertReason,omitempty"`
	Version       int64          `json:"version"`
}

func filingDTO(f *domain.Filing) FilingDTO {
	s := f.State()
	t := f.Totals()
	d := FilingDTO{ID: f.ID().String(), Declarant: s.Declarant.String(), DeclarantNIF: s.Identity.NIF, DeclarantName: s.Identity.Name,
		Form: string(s.Form), Year: s.Year, Period: s.Period.String(), Status: s.Status.String(), Number: s.Number, Reference: s.Reference,
		Recipients: t.Recipients, Perceptions: t.Perceptions.StringFixed(2), Withheld: t.Withheld.StringFixed(2), Lines: []RecipientDTO{},
		Problems: []string{}, RevertReason: s.RevertReason, Version: f.Version()}
	for _, r := range s.Recipients {
		d.Lines = append(d.Lines, RecipientDTO{Party: r.Party.String(), NIF: r.Identity.NIF, Name: r.Identity.Name, Province: r.Identity.Province,
			Key: r.Key, Payments: r.Payments, Perceptions: r.Perceptions.StringFixed(2), Withheld: r.Withheld.StringFixed(2)})
	}
	if s.Status == domain.StatusDraft {
		d.Problems = append(d.Problems, f.Problems()...)
	}
	return d
}

// content computes the identity of the declarant and the lines of a form for a period.
func (s service) content(ctx context.Context, org domain.OrganizationID, year int, p domain.Period) (domain.Identity, []domain.Recipient, error) {
	from, to := p.Bounds(year)
	ws, err := s.Withholdings.Find(ctx, domain.PaidBetween(org, from, to))
	if err != nil {
		return domain.Identity{}, nil, err
	}
	parties := []domain.PartyID{{UUID: org.UUID}}
	for _, w := range ws {
		parties = append(parties, w.State().Recipient)
	}
	ids, err := s.Identities.Identities(ctx, parties)
	if err != nil {
		return domain.Identity{}, nil, err
	}
	return ids[domain.PartyID{UUID: org.UUID}], domain.Summarize(ws, ids), nil
}

// next returns the next number of a form for a declarant and year (the counter is created on
// first use; concurrent submissions conflict and are retried).
func (s service) next(ctx context.Context, d domain.OrganizationID, form domain.Form, year int) (int64, error) {
	cs, err := s.Counters.Find(ctx, spec.And(domain.CntFieldDeclarant.Eq(d), domain.CntFieldForm.Eq(string(form)), domain.CntFieldYear.Eq(year)))
	if err != nil {
		return 0, err
	}
	var c *domain.Counter
	if len(cs) > 0 {
		c = cs[0]
	} else if c, err = domain.ReconstituteCounter(domain.NewCounterID(), d, form, year, 0); err != nil {
		return 0, err
	}
	n := c.Next()
	if err := s.Counters.Save(ctx, c); err != nil {
		return 0, err
	}
	return n, nil
}

func (s service) filingUseCases(svc *Service) {
	svc.GenerateFiling = guard(PermFilingCreate, func(ctx context.Context, c GenerateFiling) (FilingDTO, error) {
		var v fw.Validation
		org := domain.OrganizationID{UUID: parseID(&v, "organization", c.Organization)}
		period, ok := domain.ParsePeriod(c.Period)
		v.Require(ok, "period", "format", "an AEAT period: 01–12, 1T–4T or 0A")
		if err := v.Err(); err != nil {
			return FilingDTO{}, err
		}
		if err := scopeOf(ctx).check("parties.party", org, org, true); err != nil {
			return FilingDTO{}, err
		}
		form := domain.Form(c.Form)
		tp, err := s.taxpayer(ctx, GetTaxpayer{Organization: org.String()})
		if errors.Is(err, fw.ErrNotFound) {
			return FilingDTO{}, fw.Violation("fiscal.no_taxpayer", "the organization has no fiscal profile")
		} else if err != nil {
			return FilingDTO{}, err
		}
		o, ok := tp.ObligationFor(form, c.Year)
		if !ok || o.Periodicity != period.Periodicity {
			return FilingDTO{}, fw.Violation("fiscal.not_an_obligation", "the organization does not file that form for that period")
		}
		identity, lines, err := s.content(ctx, org, c.Year, period)
		if err != nil {
			return FilingDTO{}, err
		}
		existing, err := s.Filings.Find(ctx, domain.SameSlot(org, form, c.Year, period))
		if err != nil {
			return FilingDTO{}, err
		}
		if len(existing) > 0 {
			f, err := s.filings.Update(ctx, existing[0].ID(), func(_ context.Context, f *domain.Filing) error {
				if f.State().Status != domain.StatusDraft {
					return fw.Violation("fiscal.filing_submitted", "the period is already submitted: revert it first")
				}
				return f.Regenerate(identity, lines)
			})
			if err != nil {
				return FilingDTO{}, err
			}
			return filingDTO(f), nil
		}
		f, err := domain.DraftFiling(domain.NewFilingID(), org, form, c.Year, period, identity, lines)
		if err != nil {
			return FilingDTO{}, err
		}
		if err := s.filings.Create(ctx, f); err != nil {
			return FilingDTO{}, err
		}
		return filingDTO(f), nil
	}, pipeline.Transactional[GenerateFiling, FilingDTO](s.UoW))

	update := func(ctx context.Context, id domain.FilingID, fn func(context.Context, *domain.Filing) error) (FilingDTO, error) {
		sc := scopeOf(ctx)
		f, err := s.filings.Update(ctx, id, func(ctx context.Context, f *domain.Filing) error {
			if err := sc.check(domain.FilingKind, f.ID(), f.State().Declarant, true); err != nil {
				return err
			}
			return fn(ctx, f)
		})
		if err != nil {
			return FilingDTO{}, err
		}
		return filingDTO(f), nil
	}

	svc.SubmitFiling = guard(PermFilingSubmit, func(ctx context.Context, c SubmitFiling) (FilingDTO, error) {
		return update(ctx, c.ID, func(ctx context.Context, f *domain.Filing) error {
			if p := f.Problems(); len(p) > 0 {
				return f.Submit(1, c.Reference, fw.Now()) // reports the problems without consuming a number
			}
			st := f.State()
			n, err := s.next(ctx, st.Declarant, st.Form, st.Year)
			if err != nil {
				return err
			}
			return f.Submit(n, c.Reference, fw.Now())
		})
	}, retry[SubmitFiling, FilingDTO](), pipeline.Transactional[SubmitFiling, FilingDTO](s.UoW))

	svc.RevertFiling = guard(PermFilingSubmit, func(ctx context.Context, c RevertFiling) (FilingDTO, error) {
		return update(ctx, c.ID, func(_ context.Context, f *domain.Filing) error { return f.Revert(c.Reason) })
	}, retry[RevertFiling, FilingDTO]())

	svc.DiscardFiling = guard(PermFilingCreate, func(ctx context.Context, c DiscardFiling) (struct{}, error) {
		sc := scopeOf(ctx)
		return struct{}{}, s.filings.Delete(ctx, c.ID, func(_ context.Context, f *domain.Filing) error {
			if err := sc.check(domain.FilingKind, f.ID(), f.State().Declarant, true); err != nil {
				return err
			}
			return f.Discard()
		})
	})

	svc.GetFiling = guard(PermFilingRead, func(ctx context.Context, q GetFiling) (FilingDTO, error) {
		f, err := s.Filings.Get(ctx, q.ID)
		if err != nil {
			return FilingDTO{}, err
		}
		if err := scopeOf(ctx).check(domain.FilingKind, f.ID(), f.State().Declarant, false); err != nil {
			return FilingDTO{}, err
		}
		return filingDTO(f), nil
	})

	svc.SearchFilings = guard(PermFilingRead, func(ctx context.Context, q SearchFilings) (fw.Page[FilingDTO], error) {
		var v fw.Validation
		parts := []spec.Specification[*domain.Filing]{scopeOf(ctx).within()}
		if q.Organization != "" {
			parts = append(parts, domain.FilFieldDeclarant.Eq(domain.OrganizationID{UUID: parseID(&v, "organization", q.Organization)}))
		}
		if q.Form != "" {
			parts = append(parts, domain.FilFieldForm.Eq(q.Form))
		}
		if q.Year != 0 {
			parts = append(parts, domain.FilFieldYear.Eq(q.Year))
		}
		if q.Status != "" {
			st, ok := domain.ParseFilingStatus(q.Status)
			v.Require(ok, "status", "enum", "draft, submitted or reverted")
			parts = append(parts, domain.FilFieldStatus.Eq(int(st)))
		}
		if err := v.Err(); err != nil {
			return fw.Page[FilingDTO]{}, err
		}
		page, err := s.Filings.FindPage(ctx, spec.And(parts...), fw.NewPageRequest(q.Page, q.Size, domain.FilFieldYear.Desc()))
		if err != nil {
			return fw.Page[FilingDTO]{}, err
		}
		return fw.MapPage(page, filingDTO), nil
	})
}
