package distribution

import (
	"net/http"
)

// EndpointModule define el contrato con el que los subdominios o los cortes verticales registran rutas HTTP.
type EndpointModule interface {
	// RegisterRoutes monta los endpoints HTTP del módulo en el mux de destino.
	RegisterRoutes(mux *http.ServeMux)
}

// EndpointModuleFunc adapta una función a EndpointModule.
type EndpointModuleFunc func(mux *http.ServeMux)

func (fn EndpointModuleFunc) RegisterRoutes(mux *http.ServeMux) {
	fn(mux)
}
