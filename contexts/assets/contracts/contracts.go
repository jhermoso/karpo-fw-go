// Package contracts es aquello de lo que pueden depender otros contextos delimitados: el Published
// Language de Assets (Accounting contabiliza la amortización y las bajas).
package contracts

// Source es el nombre del contexto delimitado que publica.
const Source = "assets"

// DepreciationChargedV1 se publica por cada mes de amortización cargado a un activo. Period es
// YYYY-MM y Date su último día; los importes son cadenas decimales en euros con dos decimales.
type DepreciationChargedV1 struct {
	AssetID     string `json:"assetId"`
	Company     string `json:"company"`
	Code        string `json:"code"`
	Class       string `json:"class"` // land | buildings | machinery | tooling | furniture | computers | vehicles | software | other
	Period      string `json:"period"`
	Date        string `json:"date"`
	Amount      string `json:"amount"`
	Accumulated string `json:"accumulated"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (DepreciationChargedV1) IntegrationEventType() string { return "assets.depreciation-charged.v1" }

// AssetDisposedV1 se publica cuando un activo sale del registro, vendido o desechado. Result es lo
// obtenido menos el valor neto contable (coste menos amortización acumulada): positivo, un
// beneficio; negativo, una pérdida.
type AssetDisposedV1 struct {
	AssetID     string `json:"assetId"`
	Company     string `json:"company"`
	Code        string `json:"code"`
	Class       string `json:"class"`
	Kind        string `json:"kind"` // sale | scrap
	Date        string `json:"date"`
	Cost        string `json:"cost"`
	Accumulated string `json:"accumulated"`
	Proceeds    string `json:"proceeds"`
	Result      string `json:"result"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (AssetDisposedV1) IntegrationEventType() string { return "assets.asset-disposed.v1" }
