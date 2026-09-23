package domain

// DomainService is a marker interface for domain services.
// Domain services encapsulate business logic that naturally spans multiple entities or aggregate roots,
// or does not belong to a single entity. They must remain stateless.
type DomainService interface {
	isDomainService()
}

// BaseDomainService can be embedded in concrete domain service structs.
type BaseDomainService struct{}

func (BaseDomainService) isDomainService() {}
