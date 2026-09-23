package distribution

import (
	"net/http"
)

// EndpointModule defines the contract for subdomains or vertical slices to register HTTP routes.
type EndpointModule interface {
	// RegisterRoutes mounts the module's HTTP endpoints onto the target mux.
	RegisterRoutes(mux *http.ServeMux)
}

// EndpointModuleFunc adapts a function into an EndpointModule.
type EndpointModuleFunc func(mux *http.ServeMux)

func (fn EndpointModuleFunc) RegisterRoutes(mux *http.ServeMux) {
	fn(mux)
}
