package application

import (
	"context"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/exchange/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// ReserveLine is an amount of a currency a customer wants. The rate is not sent: it is quoted
// when the reservation is registered.
type ReserveLine struct {
	Currency      string `json:"currency"`
	Amount        string `json:"amount"`
	Collector     bool   `json:"collector,omitempty"`
	CollectorNote string `json:"collectorNote,omitempty"`
}

// Reserve registers a reservation of foreign cash to collect at an office.
type Reserve struct {
	Company       string        `json:"company"`
	Customer      string        `json:"customer"`
	Channel       string        `json:"channel"`
	Segment       string        `json:"segment,omitempty"` // WEB by default
	Pickup        time.Time     `json:"pickup"`
	TimeSlot      string        `json:"timeSlot,omitempty"`
	Facility      string        `json:"facility,omitempty"`
	PromotionCode string        `json:"promotionCode,omitempty"`
	Lines         []ReserveLine `json:"lines"`
}

// AdvanceReservation moves a reservation one step: email-verified, notified or completed.
type AdvanceReservation struct {
	ID domain.ReservationID `json:"-"`
	To string               `json:"to"`
}

// CancelReservation abandons a reservation.
type CancelReservation struct {
	ID     domain.ReservationID `json:"-"`
	Reason string               `json:"reason,omitempty"`
}

// ExpireDue closes the reservations of a company nobody collected in time.
type ExpireDue struct {
	Company string `json:"company"`
}

// GetReservation loads a reservation by its identity or by the reference the customer quotes.
type GetReservation struct {
	ID        domain.ReservationID
	Reference string
}

// SearchReservations searches the reservations of the caller's scope, newest first.
type SearchReservations struct {
	Company, Status, Customer, Facility, Currency string
	CreatedFrom, CreatedTo, PickupFrom, PickupTo  string
	Page, Size                                    int
}

// GetDashboard summarises the reservations of a company registered between two days.
type GetDashboard struct{ Company, From, To string }

// ReservationLineDTO is the transport form of a line.
type ReservationLineDTO struct {
	Line          int    `json:"line"`
	Currency      string `json:"currency"`
	Requested     string `json:"requested"`
	Delivered     string `json:"delivered"`
	BaseRate      string `json:"baseRate"`
	OfferedRate   string `json:"offeredRate"`
	MarginKind    string `json:"marginKind"`
	MarginValue   string `json:"marginValue"`
	Eur           string `json:"eur"`
	Collector     bool   `json:"collector,omitempty"`
	CollectorNote string `json:"collectorNote,omitempty"`
}

// StepDTO is the transport form of a change of status.
type StepDTO struct {
	Status string `json:"status"`
	At     string `json:"at"`
}

// ReservationDTO is the transport form of a reservation.
type ReservationDTO struct {
	ID            string               `json:"id"`
	Company       string               `json:"company"`
	Customer      string               `json:"customer"`
	Reference     string               `json:"reference"`
	Channel       string               `json:"channel"`
	Segment       string               `json:"segment"`
	Pickup        string               `json:"pickup"`
	TimeSlot      string               `json:"timeSlot,omitempty"`
	ExpiresAt     string               `json:"expiresAt"`
	Facility      string               `json:"facility,omitempty"`
	PromotionCode string               `json:"promotionCode,omitempty"`
	Collaborator  string               `json:"collaborator,omitempty"`
	Status        string               `json:"status"`
	Reason        string               `json:"reason,omitempty"`
	Total         string               `json:"total"`
	Currencies    []string             `json:"currencies"`
	Lines         []ReservationLineDTO `json:"lines,omitempty"`
	History       []StepDTO            `json:"history,omitempty"`
	Version       int64                `json:"version"`
}

func reservationDTO(r *domain.Reservation, detail bool) ReservationDTO {
	s := r.State()
	d := ReservationDTO{ID: r.ID().String(), Company: s.Company.String(), Customer: s.Customer.String(), Reference: s.Reference, Channel: s.Channel,
		Segment: s.Segment, Pickup: stamp(s.Pickup), TimeSlot: s.TimeSlot, ExpiresAt: stamp(s.ExpiresAt), Facility: optID(s.Facility),
		PromotionCode: s.PromotionCode, Collaborator: optID(s.Collaborator.UUID), Status: string(s.Status), Reason: s.Reason,
		Total: r.Total().StringFixed(2), Currencies: []string{}, Version: r.Version()}
	for _, l := range s.Lines {
		d.Currencies = append(d.Currencies, l.Currency)
		if detail {
			d.Lines = append(d.Lines, ReservationLineDTO{Line: l.No, Currency: l.Currency, Requested: l.Requested.StringFixed(2), Delivered: l.Delivered.StringFixed(2),
				BaseRate: l.BaseRate.StringFixed(6), OfferedRate: l.OfferedRate.StringFixed(6), MarginKind: string(l.MarginKind), MarginValue: l.MarginValue.String(),
				Eur: l.Eur.StringFixed(2), Collector: l.Collector, CollectorNote: l.CollectorNote})
		}
	}
	if detail {
		for _, h := range s.History {
			d.History = append(d.History, StepDTO{Status: string(h.Status), At: stamp(h.At)})
		}
	}
	return d
}

// ExpiredDTO is the result of closing what nobody collected.
type ExpiredDTO struct {
	Expired    int      `json:"expired"`
	References []string `json:"references"`
}

// DayDTO, CurrencyTotalDTO and FacilityTotalDTO are the breakdowns of the dashboard.
type (
	DayDTO struct {
		Date         string `json:"date"`
		Reservations int    `json:"reservations"`
		Eur          string `json:"eur"`
	}
	CurrencyTotalDTO struct {
		Currency string `json:"currency"`
		Lines    int    `json:"lines"`
		Amount   string `json:"amount"`
		Eur      string `json:"eur"`
	}
	FacilityTotalDTO struct {
		Facility     string `json:"facility,omitempty"`
		Reservations int    `json:"reservations"`
		Eur          string `json:"eur"`
	}
)

// DashboardDTO summarises a period. Totals and breakdowns count what is alive or collected:
// cancelled and expired reservations only appear in ByStatus (the C# dashboard added them up).
type DashboardDTO struct {
	Reservations int                `json:"reservations"`
	Eur          string             `json:"eur"`
	ByStatus     map[string]int     `json:"byStatus"`
	Daily        []DayDTO           `json:"daily"`
	ByCurrency   []CurrencyTotalDTO `json:"byCurrency"`
	ByFacility   []FacilityTotalDTO `json:"byFacility"`
}

func (s service) wireReservations(svc *Service) {
	d := s.Deps
	svc.Reserve = changing(d.UoW, PermReservationCreate, func(ctx context.Context, c Reserve) (ReservationDTO, error) {
		var v fw.Validation
		customer := domain.PartyID{UUID: parseID(&v, "customer", c.Customer)}
		facility := optionalID(&v, "facility", c.Facility)
		amounts := make([]vocab.Decimal, len(c.Lines))
		for i, l := range c.Lines {
			amounts[i] = parseDecimal(&v, "lines.amount", l.Amount)
		}
		v.Require(!c.Pickup.IsZero(), "pickup", "required", "the pickup time is required")
		org, err := company(ctx, &v, c.Company, true)
		if err != nil {
			return ReservationDTO{}, err
		}
		_, set, err := s.settingsOf(ctx, org)
		if err != nil {
			return ReservationDTO{}, err
		}
		// The promotion code attributes the reservation to a collaborator; it does not change the price.
		code, collaborator := strings.TrimSpace(c.PromotionCode), domain.PartyID{}
		switch {
		case set.PromotionMode == domain.PromotionDisabled:
			code = ""
		case code == "" && set.PromotionMode == domain.PromotionRequired:
			return ReservationDTO{}, fw.Violation("exchange.promotion_required", "a promotion code is required")
		case code != "" && set.ValidatePromotion:
			found := false
			if d.Collaborators != nil {
				if collaborator, found, err = d.Collaborators.ByPromotionCode(ctx, org, code); err != nil {
					return ReservationDTO{}, err
				}
			}
			if !found {
				return ReservationDTO{}, fw.Violation("exchange.promotion_unknown", "the promotion code is not that of an active collaborator")
			}
		}
		req := domain.Request{Company: org, Customer: customer, Channel: c.Channel, Segment: c.Segment, Pickup: c.Pickup, TimeSlot: c.TimeSlot,
			Facility: facility, PromotionCode: code, Collaborator: collaborator}
		if strings.TrimSpace(req.Segment) == "" {
			req.Segment = domain.DefaultSegment
		}
		// The rates are quoted here, now: what the client saw on screen is not trusted.
		for i, l := range c.Lines {
			q, err := s.quote(ctx, org, set, l.Currency, req.Segment, amounts[i])
			if err != nil {
				return ReservationDTO{}, err
			}
			req.Lines = append(req.Lines, domain.RequestLine{Quotation: q, Collector: l.Collector, CollectorNote: l.CollectorNote})
		}
		res, err := domain.RegisterReservation(domain.NewReservationID(), req, set.ExpiryHours, fw.Now())
		if err != nil {
			return ReservationDTO{}, err
		}
		if err := s.reservations.Create(ctx, res); err != nil {
			return ReservationDTO{}, err
		}
		return reservationDTO(res, true), nil
	})

	update := func(ctx context.Context, id domain.ReservationID, fn func(*domain.Reservation) (bool, error)) (ReservationDTO, error) {
		res, err := d.Reservations.Get(ctx, id)
		if err != nil {
			return ReservationDTO{}, err
		}
		if err := scopeOf(ctx).check(domain.ReservationKind, res.ID(), res.State().Company, true); err != nil {
			return ReservationDTO{}, err
		}
		// Asking for what already is changes nothing, and saves nothing.
		probe, err := domain.ReconstituteReservation(res.ID(), res.State())
		if err != nil {
			return ReservationDTO{}, err
		}
		if changed, err := fn(probe); err != nil || !changed {
			return reservationDTO(res, true), err
		}
		res, err = s.reservations.Update(ctx, id, func(_ context.Context, r *domain.Reservation) error {
			_, err := fn(r)
			return err
		})
		if err != nil {
			return ReservationDTO{}, err
		}
		return reservationDTO(res, true), nil
	}
	svc.Advance = changing(d.UoW, PermReservationProgress, func(ctx context.Context, c AdvanceReservation) (ReservationDTO, error) {
		to := domain.Status(strings.TrimSpace(c.To))
		var v fw.Validation
		v.Require(slices.Contains([]domain.Status{domain.EmailVerified, domain.Notified, domain.Completed}, to), "to", "enum",
			"email-verified, notified or completed")
		if err := v.Err(); err != nil {
			return ReservationDTO{}, err
		}
		now := fw.Now()
		return update(ctx, c.ID, func(r *domain.Reservation) (bool, error) { return r.Advance(to, now) })
	})
	svc.Cancel = changing(d.UoW, PermReservationProgress, func(ctx context.Context, c CancelReservation) (ReservationDTO, error) {
		now := fw.Now()
		return update(ctx, c.ID, func(r *domain.Reservation) (bool, error) { return r.Cancel(now, c.Reason) })
	})
	// Nothing expired a reservation in C#: this is what a scheduled job calls.
	svc.ExpireDue = changing(d.UoW, PermReservationProgress, func(ctx context.Context, c ExpireDue) (ExpiredDTO, error) {
		var v fw.Validation
		org, err := company(ctx, &v, c.Company, true)
		if err != nil {
			return ExpiredDTO{}, err
		}
		alive, err := d.Reservations.Find(ctx, spec.And(domain.ResFieldCompany.Eq(org),
			domain.ResFieldStatus.In(string(domain.Registered), string(domain.EmailVerified), string(domain.Notified))), domain.ResFieldReference.Asc())
		if err != nil {
			return ExpiredDTO{}, err
		}
		out, now := ExpiredDTO{References: []string{}}, fw.Now()
		for _, r := range alive {
			if !r.Due(now) {
				continue
			}
			if _, err := s.reservations.Update(ctx, r.ID(), func(_ context.Context, x *domain.Reservation) error {
				x.Expire(now)
				return nil
			}); err != nil {
				return ExpiredDTO{}, err
			}
			out.References = append(out.References, r.State().Reference)
		}
		out.Expired = len(out.References)
		return out, nil
	})

	svc.Get = guard(PermReservationRead, func(ctx context.Context, q GetReservation) (ReservationDTO, error) {
		var res *domain.Reservation
		var err error
		if q.ID.IsZero() {
			ref := strings.ToUpper(strings.TrimSpace(q.Reference))
			found, ferr := d.Reservations.Find(ctx, domain.ResFieldReference.Eq(ref))
			if ferr != nil {
				return ReservationDTO{}, ferr
			}
			if len(found) == 0 {
				return ReservationDTO{}, fw.NotFound(domain.ReservationKind, fw.UUID{})
			}
			res = found[0]
		} else if res, err = d.Reservations.Get(ctx, q.ID); err != nil {
			return ReservationDTO{}, err
		}
		if err := scopeOf(ctx).check(domain.ReservationKind, res.ID(), res.State().Company, false); err != nil {
			return ReservationDTO{}, err
		}
		return reservationDTO(res, true), nil
	})

	day := func(v *fw.Validation, field, s string) (vocab.Date, bool) {
		if s == "" {
			return vocab.Date{}, false
		}
		d, err := vocab.ParseDate(s)
		v.Require(err == nil, field, "format", field+" must be a date")
		return d, err == nil
	}
	svc.Search = guard(PermReservationRead, func(ctx context.Context, q SearchReservations) (fw.Page[ReservationDTO], error) {
		var v fw.Validation
		parts := []spec.Specification[*domain.Reservation]{within(scopeOf(ctx), domain.ResFieldCompany)}
		if q.Company != "" {
			parts = append(parts, domain.ResFieldCompany.Eq(domain.OrganizationID{UUID: parseID(&v, "company", q.Company)}))
		}
		if q.Status != "" {
			v.Require(slices.Contains(domain.Statuses, domain.Status(q.Status)), "status", "enum", "a status")
			parts = append(parts, domain.ResFieldStatus.Eq(q.Status))
		}
		if q.Customer != "" {
			parts = append(parts, domain.ResFieldCustomer.Eq(domain.PartyID{UUID: parseID(&v, "customer", q.Customer)}))
		}
		if q.Facility != "" {
			parts = append(parts, domain.ResFieldFacility.Eq(parseID(&v, "facility", q.Facility)))
		}
		if q.Currency != "" {
			parts = append(parts, domain.ResFieldLines.Any(domain.LineFieldCurrency.Eq(domain.NormalizeCode(q.Currency))))
		}
		if from, ok := day(&v, "createdFrom", q.CreatedFrom); ok {
			parts = append(parts, domain.ResFieldCreated.Ge(from))
		}
		if to, ok := day(&v, "createdTo", q.CreatedTo); ok {
			parts = append(parts, domain.ResFieldCreated.Le(to))
		}
		if from, ok := day(&v, "pickupFrom", q.PickupFrom); ok {
			parts = append(parts, domain.ResFieldPickup.Ge(from))
		}
		if to, ok := day(&v, "pickupTo", q.PickupTo); ok {
			parts = append(parts, domain.ResFieldPickup.Le(to))
		}
		if err := v.Err(); err != nil {
			return fw.Page[ReservationDTO]{}, err
		}
		page, err := d.Reservations.FindPage(ctx, spec.And(parts...), fw.NewPageRequest(q.Page, q.Size, domain.ResFieldCreated.Desc(), domain.ResFieldReference.Asc()))
		if err != nil {
			return fw.Page[ReservationDTO]{}, err
		}
		return fw.MapPage(page, func(r *domain.Reservation) ReservationDTO { return reservationDTO(r, false) }), nil
	})

	svc.Dashboard = guard(PermReservationRead, func(ctx context.Context, q GetDashboard) (DashboardDTO, error) {
		var v fw.Validation
		from, okFrom := day(&v, "from", q.From)
		to, okTo := day(&v, "to", q.To)
		v.Require(okFrom && okTo, "period", "required", "from and to are required")
		org, err := company(ctx, &v, q.Company, false)
		if err != nil {
			return DashboardDTO{}, err
		}
		rs, err := d.Reservations.Find(ctx, spec.And(domain.ResFieldCompany.Eq(org), domain.ResFieldCreated.Ge(from), domain.ResFieldCreated.Le(to)))
		if err != nil {
			return DashboardDTO{}, err
		}
		type sum struct {
			n           int
			amount, eur vocab.Decimal
		}
		zero := vocab.DecimalFromInt(0)
		total, daily, byCur, byFac := sum{eur: zero}, map[string]sum{}, map[string]sum{}, map[string]sum{}
		add := func(m map[string]sum, k string, n int, amount, eur vocab.Decimal) {
			x, ok := m[k]
			if !ok {
				x = sum{amount: zero, eur: zero}
			}
			m[k] = sum{n: x.n + n, amount: x.amount.Add(amount), eur: x.eur.Add(eur)}
		}
		out := DashboardDTO{ByStatus: map[string]int{}, Daily: []DayDTO{}, ByCurrency: []CurrencyTotalDTO{}, ByFacility: []FacilityTotalDTO{}}
		for _, r := range rs {
			st := r.State()
			out.ByStatus[string(st.Status)]++
			if st.Status == domain.Cancelled || st.Status == domain.Expired {
				continue
			}
			eur := r.Total()
			total = sum{n: total.n + 1, eur: total.eur.Add(eur)}
			add(daily, vocab.DateOf(st.History[0].At).String(), 1, zero, eur)
			add(byFac, optID(st.Facility), 1, zero, eur)
			for _, l := range st.Lines {
				add(byCur, l.Currency, 1, l.Delivered, l.Eur)
			}
		}
		out.Reservations, out.Eur = total.n, total.eur.StringFixed(2)
		keys := func(m map[string]sum) []string {
			ks := make([]string, 0, len(m))
			for k := range m {
				ks = append(ks, k)
			}
			sort.Strings(ks)
			return ks
		}
		for _, k := range keys(daily) {
			out.Daily = append(out.Daily, DayDTO{Date: k, Reservations: daily[k].n, Eur: daily[k].eur.StringFixed(2)})
		}
		for _, k := range keys(byCur) {
			out.ByCurrency = append(out.ByCurrency, CurrencyTotalDTO{Currency: k, Lines: byCur[k].n, Amount: byCur[k].amount.StringFixed(2), Eur: byCur[k].eur.StringFixed(2)})
		}
		sort.SliceStable(out.ByCurrency, func(i, j int) bool {
			return vocab.MustDecimal(out.ByCurrency[i].Eur).GreaterThan(vocab.MustDecimal(out.ByCurrency[j].Eur))
		})
		for _, k := range keys(byFac) {
			out.ByFacility = append(out.ByFacility, FacilityTotalDTO{Facility: k, Reservations: byFac[k].n, Eur: byFac[k].eur.StringFixed(2)})
		}
		sort.SliceStable(out.ByFacility, func(i, j int) bool { return out.ByFacility[i].Reservations > out.ByFacility[j].Reservations })
		return out, nil
	})
}
