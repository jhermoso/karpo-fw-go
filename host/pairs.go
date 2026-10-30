package host

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	orders "github.com/jhermoso/karpo-fw-go/contexts/orders/application"
	parapp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	parcontracts "github.com/jhermoso/karpo-fw-go/contexts/parties/contracts"
	purchases "github.com/jhermoso/karpo-fw-go/contexts/purchases/application"
	receivables "github.com/jhermoso/karpo-fw-go/contexts/receivables/application"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// Pairs sirve la vista por pareja empresa–tercero: «Relaciones de clientes» y «Relaciones de
// proveedores» (docs/DECISIONES-COMUNES.md, D-51 y D-73; contrato en
// docs/CONTRATO-PARTIES-V1.md §7 quinquies). Cada fila es una relación de cliente o de proveedor
// entre una empresa del grupo y un tercero, con lo que saben de esa pareja los contextos: el número
// de cliente (Parties), el límite y el bloqueo por crédito (Cobros), los bloqueos de pedido y de
// entrega (Pedidos) y el bloqueo de facturas de proveedor (Compras).
//
// Es una lectura compuesta: junta las parejas en memoria y después filtra, ordena y pagina. Basta
// para los miles de clientes de una empresa; con más haría falta una proyección de lectura.
type Pairs struct{ h *Host }

// Pairs devuelve el módulo HTTP de la vista por pareja.
func (h *Host) Pairs() Pairs { return Pairs{h: h} }

// PairRow es una fila de la vista por pareja.
type PairRow struct {
	ID                       string     `json:"id"` // la relación
	PartyID                  string     `json:"partyId"`
	Name                     string     `json:"name"`
	Kind                     string     `json:"kind"`
	TaxID                    string     `json:"taxId,omitempty"`
	Country                  string     `json:"country,omitempty"`
	Active                   bool       `json:"active"`
	Test                     bool       `json:"test"`
	Roles                    []string   `json:"roles"`
	InternalOrganizationID   string     `json:"internalOrganizationId"`
	InternalOrganizationName string     `json:"internalOrganizationName"`
	Since                    time.Time  `json:"since"`
	Until                    *time.Time `json:"until,omitempty"`
	// De cliente.
	CustomerNumber string  `json:"customerNumber,omitempty"`
	CreditLimit    *string `json:"creditLimit,omitempty"` // nulo: sin límite, o sin permiso para leer Cobros
	BlockOrders    *bool   `json:"blockOrders,omitempty"`
	BlockDelivery  *bool   `json:"blockDelivery,omitempty"`
	// De proveedor.
	SupplierNumber string `json:"supplierNumber,omitempty"`
	BlockInvoices  *bool  `json:"blockInvoices,omitempty"`
	// De los dos: los códigos de Sage y de Apiscore (D-29) y por qué está bloqueada la pareja
	// (D-122): los motivos de los bloqueos en vigor que quien llama puede leer, separados por «; ».
	SageCode     string `json:"sageCode,omitempty"`
	ApiscoreCode string `json:"apiscoreCode,omitempty"`
	BlockReason  string `json:"blockReason,omitempty"`
}

// pairFilter es el filtro de la búsqueda: el del tercero, como en la búsqueda de parties, y el de
// la pareja.
type pairFilter struct {
	Text                    string   `json:"text"`
	Kinds                   []string `json:"kinds"`
	Document                string   `json:"document"`
	Role                    string   `json:"role"`
	IncludeEndedRoles       bool     `json:"includeEndedRoles"`
	IncludeMergedParties    bool     `json:"includeMergedParties"`
	Active                  *bool    `json:"active"`
	InternalOrganizationIDs []string `json:"internalOrganizationIds"`
	IncludeEnded            bool     `json:"includeEnded"`
	CustomerNumber          string   `json:"customerNumber"`
	SupplierNumber          string   `json:"supplierNumber"`
	Blocked                 *bool    `json:"blocked"`
}

type pairSearch struct {
	Filter pairFilter `json:"filter"`
	Page   struct {
		Number int `json:"number"`
		Size   int `json:"size"`
	} `json:"page"`
	Sort struct {
		By        string `json:"by"`
		Ascending *bool  `json:"ascending"`
	} `json:"sort"`
}

// pairSorts son los campos por los que se puede ordenar la vista.
var pairSorts = []string{"name", "customerNumber", "supplierNumber", "creditLimit", "blocked", "internalOrganization", "since"}

// RegisterRoutes monta las rutas de la vista.
func (p Pairs) RegisterRoutes(mux *http.ServeMux) {
	for path, code := range map[string]string{"customer-relationships": "customer", "supplier-relationships": "supplier"} {
		mux.HandleFunc("POST /api/"+path+"/search", func(w http.ResponseWriter, r *http.Request) { p.search(w, r, code) })
		mux.HandleFunc("GET /api/"+path+"/{id}", func(w http.ResponseWriter, r *http.Request) { p.get(w, r, code) })
	}
}

func (p Pairs) search(w http.ResponseWriter, r *http.Request, code string) {
	var q pairSearch
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&q); err != nil {
		var v fw.Validation
		v.Add("body", "format", "the body is not a valid search: "+err.Error())
		distribution.WriteError(w, r, v.Err())
		return
	}
	rows, err := p.rows(r.Context(), code, q.Filter, "")
	if err != nil {
		distribution.WriteError(w, r, err)
		return
	}
	by := cmp.Or(q.Sort.By, "name")
	if !slices.Contains(pairSorts, by) {
		var v fw.Validation
		v.Add("sort.by", "enum", "sort by one of "+strings.Join(pairSorts, ", "))
		distribution.WriteError(w, r, v.Err())
		return
	}
	sortPairs(rows, by, q.Sort.Ascending == nil || *q.Sort.Ascending)
	size := q.Page.Size
	if size <= 0 {
		size = 25
	}
	if size > 500 {
		var v fw.Validation
		v.Add("page.size", "range", "a page has from 1 to 500 rows")
		distribution.WriteError(w, r, v.Err())
		return
	}
	number := max(q.Page.Number, 1)
	from := len(rows) // una página fuera de rango es una lista vacía
	if number-1 <= len(rows)/size {
		from = min((number-1)*size, len(rows))
	}
	page := struct {
		Items []PairRow `json:"items"`
		Total int       `json:"total"`
	}{Items: rows[from:min(from+size, len(rows))], Total: len(rows)}
	distribution.Respond(w, r, page, nil, http.StatusOK)
}

func (p Pairs) get(w http.ResponseWriter, r *http.Request, code string) {
	rows, err := p.rows(r.Context(), code, pairFilter{IncludeEnded: true, IncludeMergedParties: true}, r.PathValue("id"))
	if err == nil && len(rows) == 0 {
		err = fw.NotFound("parties.relationship", pathID(r.PathValue("id")))
	}
	if err != nil {
		distribution.WriteError(w, r, err)
		return
	}
	distribution.Respond(w, r, rows[0], nil, http.StatusOK)
}

// rows junta las parejas de Parties con lo que saben de ellas los demás contextos, y aplica los
// filtros de la pareja. Lo que un contexto no deja ver a quien llama (falta de permiso) se queda
// sin rellenar.
func (p Pairs) rows(ctx context.Context, code string, f pairFilter, id string) ([]PairRow, error) {
	q := parapp.SearchPairs{Code: code, Organizations: f.InternalOrganizationIDs, IncludeEnded: f.IncludeEnded, ID: id,
		Party: parapp.SearchParties{Text: f.Text, Document: f.Document, Role: f.Role, IncludeEndedRoles: f.IncludeEndedRoles,
			IncludeMergedParties: f.IncludeMergedParties}}
	for _, k := range f.Kinds {
		if k != "person" && k != "organization" {
			var v fw.Validation
			v.Add("filter.kinds", "enum", "kinds are person and organization")
			return nil, v.Err()
		}
	}
	if len(f.Kinds) == 1 { // las dos clases son todas
		q.Party.Kind = f.Kinds[0]
	}
	pairs, err := p.h.Parties.Service.Pairs.Handle(ctx, q)
	if err != nil {
		return nil, err
	}
	ids, orgs := []string{}, []string{}
	seen := map[string]bool{}
	for _, pr := range pairs {
		if !seen[pr.Party.ID] {
			seen[pr.Party.ID] = true
			ids = append(ids, pr.Party.ID)
		}
		if !slices.Contains(orgs, pr.Organization) { // pocas: las empresas del grupo
			orgs = append(orgs, pr.Organization)
		}
	}
	taxes, err := p.taxIdentities(ctx, ids)
	if err != nil {
		return nil, err
	}
	key := func(org, party string) string { return org + "/" + party }
	// Los perfiles se piden con las fusionadas: la vista ya las ha dejado fuera, o se le han pedido.
	credit, terms, suppliers := map[string]receivables.CreditDTO{}, map[string]orders.TermsDTO{}, map[string]purchases.SupplierDTO{}
	// Qué empresas deja leer cada contexto a quien llama: de las otras, el dato no se enseña.
	readCredit, readTerms, readSuppliers := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, org := range orgs {
		if code == "customer" {
			cs, err := p.h.Receivables.Service.SearchCredit.Handle(ctx, receivables.SearchCredit{Seller: org, IncludeMergedParties: true})
			if readCredit[org], err = readable(err); err != nil {
				return nil, err
			}
			for _, c := range cs {
				credit[key(c.Seller, c.Customer)] = c
			}
			ts, err := p.h.Orders.Service.SearchTerms.Handle(ctx, orders.SearchTerms{Company: org, IncludeMergedParties: true})
			if readTerms[org], err = readable(err); err != nil {
				return nil, err
			}
			for _, t := range ts {
				terms[key(t.Company, t.Customer)] = t
			}
		} else {
			ss, err := p.h.Purchases.Service.SearchSuppliers.Handle(ctx, purchases.SearchSuppliers{Company: org, IncludeMergedParties: true})
			if readSuppliers[org], err = readable(err); err != nil {
				return nil, err
			}
			for _, s := range ss {
				suppliers[key(s.Company, s.Supplier)] = s
			}
		}
	}
	out := make([]PairRow, 0, len(pairs))
	for _, pr := range pairs {
		row := PairRow{ID: pr.ID, PartyID: pr.Party.ID, Name: pr.Party.Name, Kind: pr.Party.Kind, Active: pr.Party.Active,
			Test: pr.Party.Test, Roles: []string{}, InternalOrganizationID: pr.Organization, InternalOrganizationName: pr.OrganizationName,
			Since: pr.Since, Until: pr.Until, CustomerNumber: pr.CustomerNumber, SupplierNumber: pr.SupplierNumber,
			SageCode: pr.SageCode, ApiscoreCode: pr.ApiscoreCode}
		for _, role := range pr.Party.Roles {
			if role.Active {
				row.Roles = append(row.Roles, role.Name)
			}
		}
		if t, ok := taxes[pr.Party.ID]; ok {
			row.TaxID, row.Country = t.Number, t.Country
		}
		k := key(pr.Organization, pr.Party.ID)
		org := pr.Organization
		if code == "customer" {
			c, t := credit[k], terms[k] // sin perfil: sin límite y sin bloqueo
			if readCredit[org] && c.Limited {
				limit := c.Limit
				row.CreditLimit = &limit
			}
			if readCredit[org] && readTerms[org] { // el bloqueo de pedidos sale de los dos
				blockOrders := c.Blocked || t.BlockOrders
				row.BlockOrders = &blockOrders
			}
			if readTerms[org] {
				blockDelivery := t.BlockDelivery
				row.BlockDelivery = &blockDelivery
			}
			var reasons []string
			if readCredit[org] && c.Blocked && c.BlockReason != "" {
				reasons = append(reasons, c.BlockReason)
			}
			if readTerms[org] && (t.BlockOrders || t.BlockDelivery) && t.BlockReason != "" {
				reasons = append(reasons, t.BlockReason)
			}
			row.BlockReason = strings.Join(reasons, "; ")
		} else if readSuppliers[org] {
			blocked := suppliers[k].Blocked
			row.BlockInvoices = &blocked
			row.BlockReason = suppliers[k].BlockReason
		}
		if f.CustomerNumber != "" && !strings.EqualFold(row.CustomerNumber, strings.TrimSpace(f.CustomerNumber)) {
			continue
		}
		if f.SupplierNumber != "" && !strings.EqualFold(row.SupplierNumber, strings.TrimSpace(f.SupplierNumber)) {
			continue
		}
		if f.Active != nil && row.Active != *f.Active {
			continue
		}
		if f.Blocked != nil && row.blocked() != *f.Blocked {
			continue
		}
		out = append(out, row)
	}
	return out, nil
}

// pathID es un identificador tal como llega en la ruta.
type pathID string

func (p pathID) String() string { return string(p) }

// blocked dice si la pareja tiene algún bloqueo.
func (r PairRow) blocked() bool {
	for _, b := range []*bool{r.BlockOrders, r.BlockDelivery, r.BlockInvoices} {
		if b != nil && *b {
			return true
		}
	}
	return false
}

// readable dice si quien llama pudo leer un contexto. Sin permiso, esos datos no se enseñan y no es
// un error; cualquier otro fallo, sí.
func readable(err error) (bool, error) {
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, fw.ErrForbidden):
		return false, nil
	}
	return false, err
}

// taxIdentities lee el documento fiscal y el país de los terceros, por lotes.
func (p Pairs) taxIdentities(ctx context.Context, ids []string) (map[string]parcontracts.TaxIdentity, error) {
	out := map[string]parcontracts.TaxIdentity{}
	for start := 0; start < len(ids); start += parcontracts.MaxDirectoryBatch {
		m, err := p.h.Parties.TaxIdentities.TaxIdentities(ctx, ids[start:min(start+parcontracts.MaxDirectoryBatch, len(ids))])
		if err != nil {
			return nil, fmt.Errorf("pairs: tax identities: %w", err)
		}
		for k, v := range m {
			out[k] = v
		}
	}
	return out, nil
}

// sortPairs ordena por un campo; a igualdad, por nombre y por relación, para que el orden sea
// estable entre páginas.
func sortPairs(rows []PairRow, by string, ascending bool) {
	limit := func(r PairRow) string { // los sin límite van al final al ordenar de menor a mayor
		if r.CreditLimit == nil {
			return "~"
		}
		return fmt.Sprintf("%020s", *r.CreditLimit)
	}
	field := map[string]func(a, b PairRow) int{
		"name":                 func(a, b PairRow) int { return cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)) },
		"customerNumber":       func(a, b PairRow) int { return cmp.Compare(a.CustomerNumber, b.CustomerNumber) },
		"supplierNumber":       func(a, b PairRow) int { return cmp.Compare(a.SupplierNumber, b.SupplierNumber) },
		"creditLimit":          func(a, b PairRow) int { return cmp.Compare(limit(a), limit(b)) },
		"blocked":              func(a, b PairRow) int { return boolCompare(a.blocked(), b.blocked()) },
		"internalOrganization": func(a, b PairRow) int { return cmp.Compare(a.InternalOrganizationName, b.InternalOrganizationName) },
		"since":                func(a, b PairRow) int { return a.Since.Compare(b.Since) },
	}[by]
	slices.SortStableFunc(rows, func(a, b PairRow) int {
		c := field(a, b)
		if !ascending {
			c = -c
		}
		return cmp.Or(c, cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)), cmp.Compare(a.ID, b.ID))
	})
}

func boolCompare(a, b bool) int {
	switch {
	case a == b:
		return 0
	case !a:
		return -1
	}
	return 1
}
