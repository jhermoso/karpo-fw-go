// Package contracts is what other bounded contexts may depend on: the Published Language of
// Assets (Accounting posts the depreciation and the disposals).
package contracts

// Source is the name of the publishing bounded context.
const Source = "assets"

// DepreciationChargedV1 is published for each month of depreciation charged to an asset. Period
// is YYYY-MM and Date its last day; amounts are decimal strings in euros with two decimals.
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

// IntegrationEventType implements application.IntegrationEvent.
func (DepreciationChargedV1) IntegrationEventType() string { return "assets.depreciation-charged.v1" }

// AssetDisposedV1 is published when an asset leaves the register, sold or scrapped. Result is the
// proceeds less the net book value (cost less accumulated depreciation): positive a gain,
// negative a loss.
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

// IntegrationEventType implements application.IntegrationEvent.
func (AssetDisposedV1) IntegrationEventType() string { return "assets.asset-disposed.v1" }
