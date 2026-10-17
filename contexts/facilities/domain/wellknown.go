package domain

// Tipos de instalación conocidos (WellKnownCatalog.FacilityTypes del C# y la semilla, los mismos
// GUID). Al catálogo en código del C# le faltaba Currency Exchange Office, que sólo existía en la
// semilla.
var (
	TypeWarehouse          = MustFacilityTypeID("10000000-0000-0000-0008-000000000001")
	TypePlant              = MustFacilityTypeID("10000000-0000-0000-0008-000000000002")
	TypeBuilding           = MustFacilityTypeID("10000000-0000-0000-0008-000000000003")
	TypeOffice             = MustFacilityTypeID("10000000-0000-0000-0008-000000000004")
	TypeFloor              = MustFacilityTypeID("10000000-0000-0000-0008-000000000005")
	TypeRoom               = MustFacilityTypeID("10000000-0000-0000-0008-000000000006")
	TypeDataCenter         = MustFacilityTypeID("10000000-0000-0000-0008-000000000007")
	TypeDistributionCenter = MustFacilityTypeID("10000000-0000-0000-0008-000000000008")
	TypeCurrencyExchange   = MustFacilityTypeID("10000000-0000-0000-0008-000000000009")
)

// WellKnownFacilityTypes devuelve la semilla del catálogo de tipos de instalación. Las oficinas y
// las oficinas de cambio de divisa atienden al público: exigen dirección y teléfono (la regla que
// el C# sólo comprobaba cuando un endpoint lo pedía).
func WellKnownFacilityTypes() []FacilityType {
	return []FacilityType{
		{ID: TypeWarehouse, Name: "Warehouse", Description: "Storage facility for goods and materials", Active: true},
		{ID: TypePlant, Name: "Plant", Description: "Manufacturing or production facility", Active: true},
		{ID: TypeBuilding, Name: "Building", Description: "Physical building structure", Active: true},
		{ID: TypeOffice, Name: "Office", Description: "Administrative or work office space", RequiresContact: true, Active: true},
		{ID: TypeFloor, Name: "Floor", Description: "Floor level within a building", Active: true},
		{ID: TypeRoom, Name: "Room", Description: "Individual room within a facility", Active: true},
		{ID: TypeDataCenter, Name: "Data Center", Description: "IT infrastructure and server hosting facility", Active: true},
		{ID: TypeDistributionCenter, Name: "Distribution Center", Description: "Logistics and distribution hub", Active: true},
		{ID: TypeCurrencyExchange, Name: "Currency Exchange Office", Description: "Currency exchange office", RequiresContact: true, Active: true},
	}
}
