package host

import (
	"context"

	"github.com/jhermoso/karpo-fw-go/contexts/billing"
	bilapp "github.com/jhermoso/karpo-fw-go/contexts/billing/application"
	expapp "github.com/jhermoso/karpo-fw-go/contexts/exports/application"
	expdomain "github.com/jhermoso/karpo-fw-go/contexts/exports/domain"
	"github.com/jhermoso/karpo-fw-go/contexts/orders"
	ordapp "github.com/jhermoso/karpo-fw-go/contexts/orders/application"
	"github.com/jhermoso/karpo-fw-go/contexts/parties"
	parcontracts "github.com/jhermoso/karpo-fw-go/contexts/parties/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/purchases"
	purapp "github.com/jhermoso/karpo-fw-go/contexts/purchases/application"
	"github.com/jhermoso/karpo-fw-go/contexts/receivables"
	recapp "github.com/jhermoso/karpo-fw-go/contexts/receivables/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
)

// Las listas de lo que una empresa vende y compra: facturas, pedidos, lo que deben los clientes y
// las facturas de proveedores. El C# no exportaba ninguna; cada una es la búsqueda del contexto
// dueño, leída como quien pidió el fichero, con los nombres que Parties da a las parties.

// TradeLists construye las listas de ventas y compras.
func TradeLists(p *parties.Module, b *billing.Module, o *orders.Module, r *receivables.Module, pu *purchases.Module) []expapp.Dataset {
	return []expapp.Dataset{invoiceList{p: p, b: b}, orderList{p: p, o: o}, receivableList{p: p, r: r}, purchaseInvoiceList{p: p, pu: pu}}
}

// partyNames devuelve los nombres de las parties de una página.
func partyNames(ctx context.Context, p *parties.Module, ids []string) (map[string]string, error) {
	names, seen, unique := map[string]string{}, map[string]bool{}, []string{}
	for _, id := range ids {
		if id != "" && !seen[id] {
			seen[id] = true
			unique = append(unique, id)
		}
	}
	for len(unique) > 0 {
		n := min(len(unique), parcontracts.MaxDirectoryBatch)
		some, err := p.Directory.Resolve(ctx, unique[:n])
		if err != nil {
			return nil, err
		}
		for id, ref := range some {
			names[id] = ref.Name
		}
		unique = unique[n:]
	}
	return names, nil
}

var invoiceStatuses = map[string]string{"draft": "Borrador", "issued": "Emitida", "cancelled": "Anulada", "rectified": "Rectificada"}

func spanish(words map[string]string, s string) string {
	if w, ok := words[s]; ok {
		return w
	}
	return s
}

// invoiceList es la lista de las facturas que emite una empresa.
type invoiceList struct {
	p *parties.Module
	b *billing.Module
}

func (invoiceList) Key() string                  { return "invoices" }
func (invoiceList) Title() string                { return "Facturas" }
func (invoiceList) Permission() authz.Permission { return bilapp.PermInvoiceRead }
func (invoiceList) Filters() []string {
	return []string{"seller", "customer", "status", "from", "to", mergedFilter}
}
func (invoiceList) Columns() []expdomain.Column {
	return []expdomain.Column{{Field: "number", Header: "Número"}, {Field: "kind", Header: "Tipo"}, {Field: "status", Header: "Estado"},
		{Field: "issued", Header: "Fecha", Type: expdomain.Date}, {Field: "due", Header: "Vencimiento", Type: expdomain.Date},
		{Field: "customer", Header: "Cliente"}, {Field: "nif", Header: "CIF/NIF"}, {Field: "net", Header: "Base", Type: expdomain.Number},
		{Field: "tax", Header: "Cuota", Type: expdomain.Number}, {Field: "total", Header: "Total", Type: expdomain.Number}, {Field: "currency", Header: "Divisa"}}
}

func (l invoiceList) Page(ctx context.Context, filters map[string]string, cursor string, limit int) ([]expdomain.Row, string, error) {
	found, err := l.b.Service.SearchInvoices.Handle(ctx, bilapp.SearchInvoices{Seller: filters["seller"], Customer: filters["customer"], Status: filters["status"],
		From: filters["from"], To: filters["to"], IncludeMergedParties: filters["includeMergedParties"] == "true", Page: page(cursor), Size: limit})
	if err != nil {
		return nil, "", err
	}
	ids := []string{}
	for _, it := range found.Items {
		if it.CustomerName == "" { // un borrador: el nombre se congela al emitirse
			ids = append(ids, it.Customer)
		}
	}
	names, err := partyNames(ctx, l.p, ids)
	if err != nil {
		return nil, "", err
	}
	rows := make([]expdomain.Row, 0, len(found.Items))
	for _, it := range found.Items {
		row := expdomain.Row{"number": it.Number, "kind": it.Kind, "status": spanish(invoiceStatuses, it.Status), "issued": it.IssueDate, "due": it.DueDate,
			"customer": it.CustomerName, "nif": it.CustomerNIF, "net": it.Net, "currency": it.Currency}
		if it.CustomerName == "" {
			row["customer"] = names[it.Customer]
		}
		if it.Taxes != nil {
			row["tax"], row["total"] = it.Taxes.Tax, it.Taxes.Total
		}
		rows = append(rows, row)
	}
	return rows, next(found.Number, found.TotalPages), nil
}

// orderList es la lista de los pedidos de los clientes de una empresa.
type orderList struct {
	p *parties.Module
	o *orders.Module
}

func (orderList) Key() string                  { return "orders" }
func (orderList) Title() string                { return "Pedidos" }
func (orderList) Permission() authz.Permission { return ordapp.PermOrderRead }
func (orderList) Filters() []string            { return []string{"company", "customer", "status", mergedFilter} }
func (orderList) Columns() []expdomain.Column {
	return []expdomain.Column{{Field: "number", Header: "Número"}, {Field: "date", Header: "Fecha", Type: expdomain.Date}, {Field: "customer", Header: "Cliente"},
		{Field: "reference", Header: "Referencia"}, {Field: "status", Header: "Estado"}, {Field: "total", Header: "Total", Type: expdomain.Number},
		{Field: "pending", Header: "Pendiente", Type: expdomain.Number}}
}

func (l orderList) Page(ctx context.Context, filters map[string]string, cursor string, limit int) ([]expdomain.Row, string, error) {
	found, err := l.o.Service.SearchOrders.Handle(ctx, ordapp.SearchOrders{Company: filters["company"], Customer: filters["customer"], Status: filters["status"],
		IncludeMergedParties: filters["includeMergedParties"] == "true", Page: page(cursor), Size: limit})
	if err != nil {
		return nil, "", err
	}
	ids := make([]string, 0, len(found.Items))
	for _, it := range found.Items {
		ids = append(ids, it.Customer)
	}
	names, err := partyNames(ctx, l.p, ids)
	if err != nil {
		return nil, "", err
	}
	rows := make([]expdomain.Row, 0, len(found.Items))
	for _, it := range found.Items {
		rows = append(rows, expdomain.Row{"number": it.Number, "date": it.Date, "customer": names[it.Customer], "reference": it.Reference, "status": it.Status,
			"total": it.Total, "pending": it.Pending})
	}
	return rows, next(found.Number, found.TotalPages), nil
}

// receivableList es la lista de lo que deben los clientes: una fila por plazo de cada factura.
type receivableList struct {
	p *parties.Module
	r *receivables.Module
}

func (receivableList) Key() string                  { return "receivables" }
func (receivableList) Title() string                { return "Vencimientos de cobro" }
func (receivableList) Permission() authz.Permission { return recapp.PermReceivableRead }
func (receivableList) Filters() []string            { return []string{"seller", "customer", "open", mergedFilter} }
func (receivableList) Columns() []expdomain.Column {
	return []expdomain.Column{{Field: "invoice", Header: "Factura"}, {Field: "customer", Header: "Cliente"},
		{Field: "issued", Header: "Fecha factura", Type: expdomain.Date}, {Field: "no", Header: "Plazo", Type: expdomain.Number},
		{Field: "due", Header: "Vencimiento", Type: expdomain.Date}, {Field: "amount", Header: "Importe", Type: expdomain.Number},
		{Field: "collected", Header: "Cobrado", Type: expdomain.Number}, {Field: "open", Header: "Pendiente", Type: expdomain.Number}}
}

func (l receivableList) Page(ctx context.Context, filters map[string]string, cursor string, limit int) ([]expdomain.Row, string, error) {
	found, err := l.r.Service.SearchReceivables.Handle(ctx, recapp.SearchReceivables{Seller: filters["seller"], Customer: filters["customer"],
		OpenOnly: filters["open"] == "true", IncludeMergedParties: filters["includeMergedParties"] == "true", Page: page(cursor), Size: limit})
	if err != nil {
		return nil, "", err
	}
	ids := make([]string, 0, len(found.Items))
	for _, it := range found.Items {
		ids = append(ids, it.Customer)
	}
	names, err := partyNames(ctx, l.p, ids)
	if err != nil {
		return nil, "", err
	}
	rows := []expdomain.Row{}
	for _, it := range found.Items {
		for _, i := range it.Installments {
			rows = append(rows, expdomain.Row{"invoice": it.Number, "customer": names[it.Customer], "issued": it.Issued, "no": i.No, "due": i.Due,
				"amount": i.Amount, "collected": i.Collected, "open": i.Open})
		}
	}
	return rows, next(found.Number, found.TotalPages), nil
}

// purchaseInvoiceList es la lista de las facturas que una empresa recibe de sus proveedores.
type purchaseInvoiceList struct {
	p  *parties.Module
	pu *purchases.Module
}

func (purchaseInvoiceList) Key() string                  { return "purchase-invoices" }
func (purchaseInvoiceList) Title() string                { return "Facturas de proveedor" }
func (purchaseInvoiceList) Permission() authz.Permission { return purapp.PermInvoiceRead }
func (purchaseInvoiceList) Filters() []string {
	return []string{"company", "supplier", "from", "to", mergedFilter}
}
func (purchaseInvoiceList) Columns() []expdomain.Column {
	return []expdomain.Column{{Field: "register", Header: "Registro"}, {Field: "number", Header: "Nº proveedor"}, {Field: "supplier", Header: "Proveedor"},
		{Field: "issued", Header: "Fecha", Type: expdomain.Date}, {Field: "received", Header: "Recepción", Type: expdomain.Date},
		{Field: "due", Header: "Vencimiento", Type: expdomain.Date}, {Field: "net", Header: "Base", Type: expdomain.Number},
		{Field: "tax", Header: "Cuota", Type: expdomain.Number}, {Field: "total", Header: "Total", Type: expdomain.Number},
		{Field: "withholding", Header: "Retención", Type: expdomain.Number}, {Field: "payable", Header: "A pagar", Type: expdomain.Number},
		{Field: "cancelled", Header: "Anulada", Type: expdomain.Boolean}}
}

func (l purchaseInvoiceList) Page(ctx context.Context, filters map[string]string, cursor string, limit int) ([]expdomain.Row, string, error) {
	found, err := l.pu.Service.SearchInvoices.Handle(ctx, purapp.SearchInvoices{Company: filters["company"], Supplier: filters["supplier"],
		From: filters["from"], To: filters["to"], IncludeMergedParties: filters["includeMergedParties"] == "true", Page: page(cursor), Size: limit})
	if err != nil {
		return nil, "", err
	}
	ids := make([]string, 0, len(found.Items))
	for _, it := range found.Items {
		ids = append(ids, it.Supplier)
	}
	names, err := partyNames(ctx, l.p, ids)
	if err != nil {
		return nil, "", err
	}
	rows := make([]expdomain.Row, 0, len(found.Items))
	for _, it := range found.Items {
		rows = append(rows, expdomain.Row{"register": it.Register, "number": it.SupplierNumber, "supplier": names[it.Supplier], "issued": it.Issued,
			"received": it.Received, "due": it.Due, "net": it.Net, "tax": it.Tax, "total": it.Total, "withholding": it.Withholding, "payable": it.Payable,
			"cancelled": it.Cancelled})
	}
	return rows, next(found.Number, found.TotalPages), nil
}
