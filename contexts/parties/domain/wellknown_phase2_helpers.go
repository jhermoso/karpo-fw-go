package domain

// WellKnownDocumentPolicy builds the document policy of the seed.
func WellKnownDocumentPolicy() *DocumentPolicy {
	p, err := NewDocumentPolicy(WellKnownDocumentTypes(), WellKnownCountryDocumentRules())
	if err != nil {
		panic(err)
	}
	return p
}

// WellKnownClassificationCatalog builds the classification catalog of the seed.
func WellKnownClassificationCatalog() *ClassificationCatalog {
	c, err := NewClassificationCatalog(WellKnownClassificationTypes())
	if err != nil {
		panic(err)
	}
	return c
}

// Well-known document types and classifications used by code and tests.
var (
	DocPassport         = MustDocumentTypeID("c0000000-0004-0000-0000-000000000001")
	DocNationalID       = MustDocumentTypeID("c0000000-0004-0000-0000-000000000002")
	DocTaxID            = MustDocumentTypeID("c0000000-0004-0000-0000-000000000003")
	DocDriverLicense    = MustDocumentTypeID("c0000000-0004-0000-0000-000000000004")
	DocSocialSecurity   = MustDocumentTypeID("c0000000-0004-0000-0000-000000000005")
	DocAlienRegistation = MustDocumentTypeID("c0000000-0004-0000-0000-000000000006")
	DocOther            = MustDocumentTypeID("c0000000-0004-0000-0000-000000000007")

	ClassSegmentFamily = MustClassificationTypeID("20000000-0000-0000-0005-000000000100")
	ClassRetail        = MustClassificationTypeID("20000000-0000-0000-0005-000000000101")
	ClassCorporate     = MustClassificationTypeID("20000000-0000-0000-0005-000000000102")
	ClassSectorFamily  = MustClassificationTypeID("20000000-0000-0000-0005-000000000110")
	ClassFinancial     = MustClassificationTypeID("20000000-0000-0000-0005-000000000111")
	ClassCommerce      = MustClassificationTypeID("20000000-0000-0000-0005-000000000113")
	ClassSizeMicro     = MustClassificationTypeID("20000000-0000-0000-0005-000000000121")
	ClassAmlLow        = MustClassificationTypeID("20000000-0000-0000-0005-000000000141")
)
