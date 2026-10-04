// Package products composes the Products (Productos) bounded context on a hot-swap backend.
package products

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	papp "github.com/jhermoso/karpo-fw-go/contexts/products/application"
	"github.com/jhermoso/karpo-fw-go/contexts/products/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/products/domain"
	"github.com/jhermoso/karpo-fw-go/contexts/products/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/messaging"
	"github.com/jhermoso/karpo-fw-go/pkg/application/outbox"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
)

// Module is the composed context.
type Module struct {
	Service           *papp.Service
	Catalog           contracts.Catalog
	Pricing           contracts.Pricing
	IntegrationOutbox application.OutboxStore
	Audit             application.AuditLog
}

// Compose builds the context on sw.
func Compose(sw *hotswap.Switch) *Module {
	integration := hotswap.Outbox(sw, infrastructure.IntegrationOutboxFactory)
	audit := hotswap.AuditLog(sw, infrastructure.AuditLogFactory)
	d := papp.Deps{
		Products: hotswap.Repository(sw, infrastructure.ProductRepositoryFactory), Categories: hotswap.Repository(sw, infrastructure.CategoryRepositoryFactory),
		PriceLists: hotswap.Repository(sw, infrastructure.PriceListRepositoryFactory), UoW: sw, Audit: audit,
		Recorder: outbox.Recorders(outbox.NewRecorder(hotswap.Outbox(sw, infrastructure.OutboxFactory)),
			papp.Publications(messaging.NewRecorder(contracts.Source, integration))),
	}
	return &Module{Service: papp.NewService(d), Catalog: papp.CatalogPort{Repo: d.Products},
		Pricing: papp.PricingPort{Products: d.Products, PriceLists: d.PriceLists}, IntegrationOutbox: integration, Audit: audit}
}

// Relay forwards the Published Language to a transport.
func (m *Module) Relay(sender application.MessageSender, opts ...outbox.RelayOption) *outbox.Relay {
	return messaging.NewRelay(contracts.Source, m.IntegrationOutbox, sender, opts...)
}

func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var val fw.Validation
		val.Add("body", "json", err.Error())
		return val.Err()
	}
	return nil
}

func badID(w http.ResponseWriter, r *http.Request) {
	distribution.WriteError(w, r, fmt.Errorf("%w: invalid id", fw.ErrValidation))
}

func command[ID, In, Out any](parse func(string) (ID, error), set func(*In, ID), run func(context.Context, In) (Out, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := parse(r.PathValue("id"))
		if err != nil {
			badID(w, r)
			return
		}
		var c In
		if r.ContentLength != 0 {
			if err := decode(r, &c); err != nil {
				distribution.WriteError(w, r, err)
				return
			}
		}
		set(&c, id)
		out, err := run(r.Context(), c)
		distribution.Respond(w, r, out, err, http.StatusOK)
	}
}

func create[In, Out any](run func(context.Context, In) (Out, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var c In
		if err := decode(r, &c); err != nil {
			distribution.WriteError(w, r, err)
			return
		}
		out, err := run(r.Context(), c)
		distribution.Respond(w, r, out, err, http.StatusCreated)
	}
}

func get[ID, Out any](parse func(string) (ID, error), run func(context.Context, ID) (Out, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := parse(r.PathValue("id"))
		if err != nil {
			badID(w, r)
			return
		}
		out, err := run(r.Context(), id)
		distribution.Respond(w, r, out, err, http.StatusOK)
	}
}

// UnitDTO is the transport form of a unit of measure.
type UnitDTO struct {
	Code      string `json:"code"`
	Name      string `json:"name"`
	Dimension string `json:"dimension"`
}

// RegisterRoutes implements distribution.EndpointModule.
func (m *Module) RegisterRoutes(mux *http.ServeMux) {
	svc := m.Service
	prod, cat, pl := domain.ParseProductID, domain.ParseCategoryID, domain.ParsePriceListID

	mux.HandleFunc("GET /api/products/units", func(w http.ResponseWriter, r *http.Request) {
		out := make([]UnitDTO, 0, len(domain.Units))
		for _, u := range domain.Units {
			out = append(out, UnitDTO{Code: u.Code, Name: u.Name, Dimension: u.Dimension})
		}
		distribution.Respond(w, r, out, nil, http.StatusOK)
	})

	mux.HandleFunc("POST /api/products/products", create(svc.RegisterProduct.Handle))
	mux.HandleFunc("GET /api/products/products", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		atoi := func(k string) int { n, _ := strconv.Atoi(q.Get(k)); return n }
		out, err := svc.SearchProducts.Handle(r.Context(), papp.SearchProducts{Company: q.Get("company"), Text: q.Get("q"), Kind: q.Get("kind"),
			Category: q.Get("category"), Barcode: q.Get("barcode"), Page: atoi("page"), Size: atoi("size")})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("GET /api/products/products/{id}", get(prod, func(ctx context.Context, id domain.ProductID) (papp.ProductDTO, error) {
		return svc.GetProduct.Handle(ctx, papp.GetProduct{ID: id})
	}))
	mux.HandleFunc("PUT /api/products/products/{id}", command(prod, func(c *papp.ChangeProduct, id domain.ProductID) { c.ID = id }, svc.ChangeProduct.Handle))
	mux.HandleFunc("POST /api/products/products/{id}/discontinue", command(prod, func(c *papp.DiscontinueProduct, id domain.ProductID) { c.ID = id },
		svc.DiscontinueProduct.Handle))

	mux.HandleFunc("POST /api/products/categories", create(svc.CreateCategory.Handle))
	mux.HandleFunc("GET /api/products/categories", func(w http.ResponseWriter, r *http.Request) {
		out, err := svc.SearchCategories.Handle(r.Context(), papp.SearchCategories{Company: r.URL.Query().Get("company")})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("PUT /api/products/categories/{id}/name", command(cat, func(c *papp.RenameCategory, id domain.CategoryID) { c.ID = id }, svc.RenameCategory.Handle))

	mux.HandleFunc("POST /api/products/price-lists", create(svc.CreatePriceList.Handle))
	mux.HandleFunc("GET /api/products/price-lists", func(w http.ResponseWriter, r *http.Request) {
		out, err := svc.SearchPriceLists.Handle(r.Context(), papp.SearchPriceLists{Company: r.URL.Query().Get("company")})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("GET /api/products/price-lists/{id}", get(pl, func(ctx context.Context, id domain.PriceListID) (papp.PriceListDTO, error) {
		return svc.GetPriceList.Handle(ctx, papp.GetPriceList{ID: id})
	}))
	mux.HandleFunc("POST /api/products/price-lists/{id}/prices", command(pl, func(c *papp.SetPrice, id domain.PriceListID) { c.ID = id }, svc.SetPrice.Handle))
	mux.HandleFunc("POST /api/products/price-lists/{id}/prices/remove", command(pl, func(c *papp.RemovePrice, id domain.PriceListID) { c.ID = id }, svc.RemovePrice.Handle))
	mux.HandleFunc("PUT /api/products/price-lists/{id}/active", command(pl, func(c *papp.SetPriceListActive, id domain.PriceListID) { c.ID = id },
		svc.SetPriceListActive.Handle))
	mux.HandleFunc("GET /api/products/quote", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		out, err := svc.Quote.Handle(r.Context(), papp.GetQuote{Company: q.Get("company"), Product: q.Get("product"), PriceList: q.Get("priceList"),
			Quantity: q.Get("quantity"), On: q.Get("on")})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
}

var _ distribution.EndpointModule = (*Module)(nil)
