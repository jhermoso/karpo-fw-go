package mailbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	app "github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/application/orchestration"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
)

// Listener es un consumidor del anfitrión: recoge un mensaje una vez, en su propia unidad de trabajo.
type Listener interface {
	app.MessageHandler
	Name() string
}

// Office guarda los buzones de los oyentes de un anfitrión.
type Office struct {
	deliveries Repository
	orch       *orchestration.Orchestrator[ID, *Delivery]
	listeners  map[string]Listener
	givenUp    func(context.Context, DTO)
	// turn deja pasar de uno en uno lo que recorre o cambia las entregas guardadas: la ronda del
	// planificador, el reintento que pide alguien por HTTP y el descarte. Sin él, dos recorridos a
	// la vez leerían las mismas entregas y el oyente recibiría dos veces el mismo mensaje. Vale para
	// un proceso, que es como se despliega hoy; con varios procesos sobre la misma base habrá que
	// reclamar cada entrega en la base antes de entregarla.
	turn chan struct{}
}

// take espera su turno, o a que se acabe ctx.
func (o *Office) take(ctx context.Context) (release func(), err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case o.turn <- struct{}{}:
		return func() { <-o.turn }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// OnGivenUp fija a quién se avisa cuando se abandona una entrega: el único momento en que alguien
// tiene que saberlo, porque desde entonces nada pasa por sí solo. fn no debe hacer fallar la ronda:
// se le avisa, nada más.
func (o *Office) OnGivenUp(fn func(context.Context, DTO)) { o.givenUp = fn }

// New construye la oficina sobre sw.
func New(sw *hotswap.Switch) *Office {
	repo := hotswap.Repository(sw, RepositoryFactory)
	return &Office{deliveries: repo, orch: orchestration.New[ID, *Delivery](repo, sw), listeners: map[string]Listener{}, turn: make(chan struct{}, 1)}
}

// Box es el buzón de un oyente: a lo que entrega el transporte en vez de al oyente.
type Box struct {
	office   *Office
	listener Listener
}

// For devuelve el buzón de un oyente.
func (o *Office) For(l Listener) *Box {
	o.listeners[l.Name()] = l
	return &Box{office: o, listener: l}
}

// Name es el nombre por el que el transporte conoce al oyente.
func (b *Box) Name() string { return b.listener.Name() }

func codeOf(err error) string {
	var rule *fw.RuleViolationError
	switch {
	case errors.As(err, &rule):
		return rule.Code
	case errors.Is(err, fw.ErrValidation):
		return "validation"
	case errors.Is(err, fw.ErrNotFound):
		return "not-found"
	case errors.Is(err, fw.ErrConflict):
		return "conflict"
	}
	return "error"
}

func (o *Office) open(ctx context.Context, more ...spec.Specification[*Delivery]) ([]*Delivery, error) {
	return o.deliveries.Find(ctx, spec.And(append([]spec.Specification[*Delivery]{FieldStatus.In(string(Waiting), string(GivenUp))}, more...)...))
}

func stateOf(consumer string, env app.Envelope) State {
	return State{Consumer: consumer, EventType: env.Type, EnvelopeID: env.ID, Source: env.Source, Subject: env.Subject, OccurredAt: env.OccurredAt.UTC(),
		CorrelationID: env.CorrelationID, CausationID: env.CausationID, Data: string(env.Data)}
}

// HandleMessage implementa application.MessageHandler: el oyente recoge el mensaje, o se le guarda.
// Sólo es error no poder guardarlo: entonces el publicador lo envía otra vez, lo que no tiene
// peligro. Se ha de llamar fuera de toda unidad de trabajo, como el propio oyente.
func (b *Box) HandleMessage(ctx context.Context, env app.Envelope) error {
	o, name := b.office, b.listener.Name()
	// Enviado otra vez después de guardarlo: ya está aquí (con su identidad, o con su huella si no
	// cabía). Uno sin identidad no se puede reconocer: no se busca.
	if env.ID != "" {
		if known, err := o.deliveries.Exists(ctx, spec.And(FieldConsumer.Eq(name), FieldEnvelope.Eq(fw.EnvelopeKey(env.ID)))); err != nil || known {
			return err
		}
	}
	// Detrás de lo que espera sobre lo mismo: un oyente recibe en orden los mensajes de una cosa.
	if env.Subject != "" {
		ahead, err := o.open(ctx, FieldConsumer.Eq(name), FieldSubject.Eq(env.Subject))
		if err != nil {
			return err
		}
		if len(ahead) > 0 {
			d, err := Queue(NewID(), stateOf(name, env), fw.Now())
			if err != nil {
				return err
			}
			return o.orch.Create(ctx, d)
		}
	}
	herr := b.listener.HandleMessage(ctx, env)
	if herr == nil || ctx.Err() != nil {
		return ctx.Err() // un proceso que se está parando no guarda nada: el publicador aún tiene el mensaje
	}
	d, err := Keep(NewID(), stateOf(name, env), codeOf(herr), herr.Error(), fw.Now())
	if err != nil {
		return errors.Join(herr, err)
	}
	return o.orch.Create(ctx, d)
}

func envelopeOf(s State) app.Envelope {
	return app.Envelope{ID: s.EnvelopeID, Type: s.EventType, Source: s.Source, Subject: s.Subject, OccurredAt: s.OccurredAt, CorrelationID: s.CorrelationID,
		CausationID: s.CausationID, Data: json.RawMessage(s.Data)}
}

func inOrder(ds []*Delivery) {
	slices.SortFunc(ds, func(a, b *Delivery) int {
		x, y := a.State(), b.State()
		if c := x.OccurredAt.Compare(y.OccurredAt); c != 0 {
			return c
		}
		if c := x.ReceivedAt.Compare(y.ReceivedAt); c != 0 {
			return c
		}
		return strings.Compare(a.ID().String(), b.ID().String())
	})
}

// Redelivered es lo que hizo una ronda de reentrega.
type Redelivered struct {
	Delivered int `json:"delivered"`
	Waiting   int `json:"waiting"`
	GivenUp   int `json:"givenUp"`
}

// Redeliver vuelve a intentar lo que espera y ya toca, en el orden en que pasó. Para cada oyente,
// un mensaje que aún no se puede entregar (o que se abandonó) retiene a los que vinieron después
// sobre lo mismo. Con force, también se intenta lo que aún no toca. Si otro recorrido está en
// marcha, espera a que acabe.
func (o *Office) Redeliver(ctx context.Context, force bool) (Redelivered, error) {
	release, err := o.take(ctx)
	if err != nil {
		return Redelivered{}, err
	}
	defer release()
	return o.redeliver(ctx, force)
}

// redeliver es Redeliver sin pedir turno: quien la llama ya lo tiene.
func (o *Office) redeliver(ctx context.Context, force bool) (Redelivered, error) {
	var out Redelivered
	all, err := o.open(ctx)
	if err != nil {
		return out, err
	}
	inOrder(all)
	now := fw.Now()
	held := map[string]bool{}
	for _, d := range all {
		st := d.State()
		chain := st.Consumer + "|" + st.Subject
		blocked := st.Subject != "" && held[chain]
		listener, known := o.listeners[st.Consumer]
		switch {
		case st.Status == GivenUp:
			held[chain] = true
			out.GivenUp++
			continue
		case blocked || !known || (!force && st.NextAttempt.After(now)):
			held[chain] = true
			out.Waiting++
			continue
		}
		herr := listener.HandleMessage(ctx, envelopeOf(st))
		if err := ctx.Err(); err != nil {
			return out, err
		}
		if herr == nil {
			if err := o.orch.Delete(ctx, d.ID(), func(context.Context, *Delivery) error { return nil }); err != nil {
				return out, err
			}
			out.Delivered++
			continue
		}
		after, err := o.orch.Update(ctx, d.ID(), func(_ context.Context, x *Delivery) error {
			x.Refused(codeOf(herr), herr.Error(), fw.Now())
			return nil
		})
		if err != nil {
			return out, err
		}
		held[chain] = true
		if after.State().Status == GivenUp {
			out.GivenUp++
			if o.givenUp != nil {
				o.givenUp(ctx, dto(after))
			}
		} else {
			out.Waiting++
		}
	}
	return out, nil
}

// Comandos y consultas: para un administrador global, como las historias.
type (
	// Search lista lo que está guardado, lo más antiguo primero.
	Search struct {
		Consumer, Status string
		Page, Size       int
	}
	// Retry devuelve a las que se intentan las entregas abandonadas (una, o todas) e intenta ahora
	// todo lo que espera.
	Retry struct {
		ID string `json:"id,omitempty"`
	}
	// Discard abandona una entrega para siempre, diciendo por qué.
	Discard struct {
		ID   ID     `json:"-"`
		Note string `json:"note"`
	}
)

// DTO es la forma de transporte de una entrega.
type DTO struct {
	ID          string          `json:"id"`
	Consumer    string          `json:"consumer"`
	EventType   string          `json:"eventType"`
	EnvelopeID  string          `json:"envelopeId"`
	Source      string          `json:"source,omitempty"`
	Subject     string          `json:"subject,omitempty"`
	OccurredAt  string          `json:"occurredAt,omitempty"`
	ReceivedAt  string          `json:"receivedAt"`
	Status      string          `json:"status"`
	Code        string          `json:"code,omitempty"`
	Reason      string          `json:"reason,omitempty"`
	Attempts    int             `json:"attempts"`
	NextAttempt string          `json:"nextAttempt,omitempty"`
	ResolvedAt  string          `json:"resolvedAt,omitempty"`
	Note        string          `json:"note,omitempty"`
	Data        json.RawMessage `json:"data"`
	Version     int64           `json:"version"`
}

func rfc3339(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func dto(d *Delivery) DTO {
	s := d.State()
	return DTO{ID: d.ID().String(), Consumer: s.Consumer, EventType: s.EventType, EnvelopeID: s.EnvelopeID, Source: s.Source, Subject: s.Subject, OccurredAt: rfc3339(s.OccurredAt),
		ReceivedAt: rfc3339(s.ReceivedAt), Status: string(s.Status), Code: s.Code, Reason: s.Reason, Attempts: s.Attempts,
		NextAttempt: rfc3339(s.NextAttempt), ResolvedAt: rfc3339(s.ResolvedAt), Note: s.Note, Data: json.RawMessage(s.Data), Version: d.Version()}
}

func admin(ctx context.Context) error {
	if ac, ok := authz.FromContext(ctx); !ok || !ac.GlobalAdmin {
		return fmt.Errorf("%w: the mailboxes of the listeners are kept by a global administrator", fw.ErrForbidden)
	}
	return nil
}

// Search lista las entregas guardadas.
func (o *Office) Search(ctx context.Context, q Search) (fw.Page[DTO], error) {
	if err := admin(ctx); err != nil {
		return fw.Page[DTO]{}, err
	}
	var v fw.Validation
	parts := []spec.Specification[*Delivery]{spec.All[*Delivery]()}
	if q.Consumer != "" {
		parts = append(parts, FieldConsumer.Eq(q.Consumer))
	}
	if q.Status != "" {
		v.Require(slices.Contains(Statuses, Status(q.Status)), "status", "enum", "waiting, given-up or discarded")
		parts = append(parts, FieldStatus.Eq(q.Status))
	}
	if err := v.Err(); err != nil {
		return fw.Page[DTO]{}, err
	}
	page, err := o.deliveries.FindPage(ctx, spec.And(parts...), fw.NewPageRequest(q.Page, q.Size, FieldReceived.Asc()))
	if err != nil {
		return fw.Page[DTO]{}, err
	}
	return fw.MapPage(page, dto), nil
}

// Count es cuántas entregas tiene un oyente en espera y abandonadas.
type Count struct {
	Consumer string `json:"consumer"`
	Waiting  int    `json:"waiting"`
	GivenUp  int    `json:"givenUp"`
}

// Summary cuenta lo que tiene pendiente cada oyente, primero los que tienen algo abandonado: lo que
// muestra una pantalla para saber de un vistazo si alguien tiene que mirar.
func (o *Office) Summary(ctx context.Context) ([]Count, error) {
	if err := admin(ctx); err != nil {
		return nil, err
	}
	open, err := o.open(ctx)
	if err != nil {
		return nil, err
	}
	by := map[string]*Count{}
	for _, d := range open {
		st := d.State()
		c := by[st.Consumer]
		if c == nil {
			c = &Count{Consumer: st.Consumer}
			by[st.Consumer] = c
		}
		if st.Status == GivenUp {
			c.GivenUp++
		} else {
			c.Waiting++
		}
	}
	out := make([]Count, 0, len(by))
	for _, c := range by {
		out = append(out, *c)
	}
	slices.SortFunc(out, func(a, b Count) int {
		if a.GivenUp != b.GivenUp {
			return b.GivenUp - a.GivenUp
		}
		return strings.Compare(a.Consumer, b.Consumer)
	})
	return out, nil
}

// Retry vuelve a intentar lo que alguien pide.
func (o *Office) Retry(ctx context.Context, c Retry) (Redelivered, error) {
	if err := admin(ctx); err != nil {
		return Redelivered{}, err
	}
	release, err := o.take(ctx)
	if err != nil {
		return Redelivered{}, err
	}
	defer release()
	again := func(_ context.Context, d *Delivery) error { d.Again(fw.Now()); return nil }
	if c.ID != "" {
		id, err := ParseID(c.ID)
		if err != nil {
			var v fw.Validation
			v.Add("id", "format", "the identity of a delivery")
			return Redelivered{}, v.Err()
		}
		if _, err := o.orch.Update(ctx, id, again); err != nil {
			return Redelivered{}, err
		}
	} else {
		given, err := o.deliveries.Find(ctx, FieldStatus.Eq(string(GivenUp)))
		if err != nil {
			return Redelivered{}, err
		}
		for _, d := range given {
			if _, err := o.orch.Update(ctx, d.ID(), again); err != nil {
				return Redelivered{}, err
			}
		}
	}
	return o.redeliver(ctx, true)
}

// Discard abandona una entrega para siempre. Espera a que acabe el recorrido en marcha, para no
// descartar lo que se está entregando.
func (o *Office) Discard(ctx context.Context, c Discard) (DTO, error) {
	if err := admin(ctx); err != nil {
		return DTO{}, err
	}
	release, err := o.take(ctx)
	if err != nil {
		return DTO{}, err
	}
	defer release()
	d, err := o.orch.Update(ctx, c.ID, func(_ context.Context, d *Delivery) error { return d.Discard(c.Note, fw.Now()) })
	if err != nil {
		return DTO{}, err
	}
	return dto(d), nil
}

func decode(r *http.Request, v any) error {
	if r.ContentLength == 0 {
		return nil
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var val fw.Validation
		val.Add("body", "json", err.Error())
		return val.Err()
	}
	return nil
}

// RegisterRoutes implementa distribution.EndpointModule.
func (o *Office) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/deliveries", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		atoi := func(k string) int { n, _ := strconv.Atoi(q.Get(k)); return n }
		out, err := o.Search(r.Context(), Search{Consumer: q.Get("consumer"), Status: q.Get("status"), Page: atoi("page"), Size: atoi("size")})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("GET /api/deliveries/summary", func(w http.ResponseWriter, r *http.Request) {
		out, err := o.Summary(r.Context())
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("POST /api/deliveries/retry", func(w http.ResponseWriter, r *http.Request) {
		var c Retry
		if err := decode(r, &c); err != nil {
			distribution.WriteError(w, r, err)
			return
		}
		out, err := o.Retry(r.Context(), c)
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("POST /api/deliveries/{id}/discard", func(w http.ResponseWriter, r *http.Request) {
		id, err := ParseID(r.PathValue("id"))
		if err != nil {
			distribution.WriteError(w, r, fmt.Errorf("%w: invalid id", fw.ErrValidation))
			return
		}
		c := Discard{ID: id}
		if err := decode(r, &c); err != nil {
			distribution.WriteError(w, r, err)
			return
		}
		c.ID = id
		out, err := o.Discard(r.Context(), c)
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
}

var _ distribution.EndpointModule = (*Office)(nil)
