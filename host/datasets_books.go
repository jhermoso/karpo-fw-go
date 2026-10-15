package host

import (
	"context"

	"github.com/jhermoso/karpo-fw-go/contexts/accounting"
	accapp "github.com/jhermoso/karpo-fw-go/contexts/accounting/application"
	"github.com/jhermoso/karpo-fw-go/contexts/assets"
	astapp "github.com/jhermoso/karpo-fw-go/contexts/assets/application"
	expapp "github.com/jhermoso/karpo-fw-go/contexts/exports/application"
	expdomain "github.com/jhermoso/karpo-fw-go/contexts/exports/domain"
	"github.com/jhermoso/karpo-fw-go/contexts/parties"
	"github.com/jhermoso/karpo-fw-go/contexts/payments"
	payapp "github.com/jhermoso/karpo-fw-go/contexts/payments/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
)

// The lists of the books of a company: its journal, what it owes and its fixed assets. As the
// others, each is the search of the context that owns it, read as who asked for the file.

// BookLists builds the lists of the books.
func BookLists(p *parties.Module, a *accounting.Module, pay *payments.Module, as *assets.Module) []expapp.Dataset {
	return []expapp.Dataset{journalList{p: p, a: a}, payableList{p: p, pay: pay}, assetList{as: as}}
}

// journalList is the journal of a company: a row per line of each entry.
type journalList struct {
	p *parties.Module
	a *accounting.Module
}

func (journalList) Key() string                  { return "journal" }
func (journalList) Title() string                { return "Libro diario" }
func (journalList) Permission() authz.Permission { return accapp.PermEntryRead }
func (journalList) Filters() []string            { return []string{"company", "from", "to"} }
func (journalList) Columns() []expdomain.Column {
	return []expdomain.Column{{Field: "year", Header: "Ejercicio", Type: expdomain.Number}, {Field: "number", Header: "Asiento", Type: expdomain.Number},
		{Field: "date", Header: "Fecha", Type: expdomain.Date}, {Field: "account", Header: "Cuenta"}, {Field: "party", Header: "Tercero"},
		{Field: "description", Header: "Concepto"}, {Field: "debit", Header: "Debe", Type: expdomain.Number},
		{Field: "credit", Header: "Haber", Type: expdomain.Number}, {Field: "source", Header: "Origen"}}
}

func (l journalList) Page(ctx context.Context, filters map[string]string, cursor string, limit int) ([]expdomain.Row, string, error) {
	found, err := l.a.Service.SearchEntries.Handle(ctx, accapp.SearchEntries{Company: filters["company"], From: filters["from"], To: filters["to"],
		Page: page(cursor), Size: limit})
	if err != nil {
		return nil, "", err
	}
	ids := []string{}
	for _, e := range found.Items {
		for _, ln := range e.Lines {
			ids = append(ids, ln.Party)
		}
	}
	names, err := partyNames(ctx, l.p, ids)
	if err != nil {
		return nil, "", err
	}
	rows := []expdomain.Row{}
	for _, e := range found.Items {
		for _, ln := range e.Lines {
			description := ln.Description
			if description == "" {
				description = e.Description
			}
			rows = append(rows, expdomain.Row{"year": e.Year, "number": e.Number, "date": e.Date, "account": ln.Account, "party": names[ln.Party],
				"description": description, "debit": ln.Debit, "credit": ln.Credit, "source": e.Source})
		}
	}
	return rows, next(found.Number, found.TotalPages), nil
}

// payableList is the list of what a company owes: to its suppliers, to its people and to the
// tax authorities.
type payableList struct {
	p   *parties.Module
	pay *payments.Module
}

func (payableList) Key() string                  { return "payables" }
func (payableList) Title() string                { return "Vencimientos de pago" }
func (payableList) Permission() authz.Permission { return payapp.PermPayableRead }
func (payableList) Filters() []string            { return []string{"company", "payee", "kind", "dueTo", "open"} }
func (payableList) Columns() []expdomain.Column {
	return []expdomain.Column{{Field: "document", Header: "Documento"}, {Field: "kind", Header: "Tipo"}, {Field: "payee", Header: "Acreedor"},
		{Field: "description", Header: "Concepto"}, {Field: "issued", Header: "Fecha", Type: expdomain.Date},
		{Field: "due", Header: "Vencimiento", Type: expdomain.Date}, {Field: "amount", Header: "Importe", Type: expdomain.Number},
		{Field: "paid", Header: "Pagado", Type: expdomain.Number}, {Field: "open", Header: "Pendiente", Type: expdomain.Number},
		{Field: "cancelled", Header: "Anulado", Type: expdomain.Boolean}}
}

func (l payableList) Page(ctx context.Context, filters map[string]string, cursor string, limit int) ([]expdomain.Row, string, error) {
	found, err := l.pay.Service.SearchPayables.Handle(ctx, payapp.SearchPayables{Company: filters["company"], Payee: filters["payee"], Kind: filters["kind"],
		DueTo: filters["dueTo"], OpenOnly: filters["open"] == "true", Page: page(cursor), Size: limit})
	if err != nil {
		return nil, "", err
	}
	ids := make([]string, 0, len(found.Items))
	for _, it := range found.Items {
		ids = append(ids, it.Payee)
	}
	names, err := partyNames(ctx, l.p, ids)
	if err != nil {
		return nil, "", err
	}
	rows := make([]expdomain.Row, 0, len(found.Items))
	for _, it := range found.Items {
		payee := names[it.Payee]
		if payee == "" {
			payee = it.Payee // a tax authority: it is nobody of Parties
		}
		rows = append(rows, expdomain.Row{"document": it.Document, "kind": it.Kind, "payee": payee, "description": it.Description, "issued": it.Issued,
			"due": it.Due, "amount": it.Amount, "paid": it.Paid, "open": it.Open, "cancelled": it.Cancelled})
	}
	return rows, next(found.Number, found.TotalPages), nil
}

// assetList is the register of the fixed assets of a company.
type assetList struct{ as *assets.Module }

func (assetList) Key() string                  { return "assets" }
func (assetList) Title() string                { return "Inmovilizado" }
func (assetList) Permission() authz.Permission { return astapp.PermAssetRead }
func (assetList) Filters() []string            { return []string{"company", "class", "inService"} }
func (assetList) Columns() []expdomain.Column {
	return []expdomain.Column{{Field: "code", Header: "Código"}, {Field: "name", Header: "Nombre"}, {Field: "class", Header: "Clase"},
		{Field: "serial", Header: "Nº serie"}, {Field: "acquired", Header: "Adquisición", Type: expdomain.Date},
		{Field: "inService", Header: "Puesta en servicio", Type: expdomain.Date}, {Field: "cost", Header: "Coste", Type: expdomain.Number},
		{Field: "residual", Header: "Valor residual", Type: expdomain.Number}, {Field: "life", Header: "Vida (meses)", Type: expdomain.Number},
		{Field: "accumulated", Header: "Amortización acumulada", Type: expdomain.Number},
		{Field: "net", Header: "Valor neto", Type: expdomain.Number}, {Field: "status", Header: "Estado"},
		{Field: "disposed", Header: "Baja", Type: expdomain.Date}}
}

func (l assetList) Page(ctx context.Context, filters map[string]string, cursor string, limit int) ([]expdomain.Row, string, error) {
	found, err := l.as.Service.SearchAssets.Handle(ctx, astapp.SearchAssets{Company: filters["company"], Class: filters["class"],
		InService: filters["inService"] == "true", Page: page(cursor), Size: limit})
	if err != nil {
		return nil, "", err
	}
	rows := make([]expdomain.Row, 0, len(found.Items))
	for _, it := range found.Items {
		rows = append(rows, expdomain.Row{"code": it.Code, "name": it.Name, "class": it.Class, "serial": it.Serial, "acquired": it.Acquired,
			"inService": it.InService, "cost": it.Cost, "residual": it.Residual, "life": it.LifeMonths, "accumulated": it.Accumulated,
			"net": it.NetBookValue, "status": it.Status, "disposed": it.Disposed})
	}
	return rows, next(found.Number, found.TotalPages), nil
}
