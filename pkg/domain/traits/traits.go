// Package traits provides the composable cross-cutting traits of Karpo aggregates: the Go
// replacement of the C# BusinessEntity base class and its "adjectives" (IToggleable, IExpirable,
// IAuditable, ITesteable, INamed, IDescriptable, IComentable, IRegulated...).
//
// Instead of one base class that forces eight interfaces on every entity, an aggregate embeds
// only the traits it needs:
//
//	type Party struct {
//		domain.BaseAggregateRoot[PartyID]
//		traits.Activation // IsActive / Activate / Deactivate
//		traits.Validity   // valid period, expiration
//		traits.Audited    // created/modified by and at, stamped by the application layer
//		...
//	}
//
// Each trait is state plus behaviour with no hidden side effects: it never raises events or
// reads ambient context. Aggregates decide which domain event to raise when a trait method
// reports a change, and the application layer stamps audit data from the request context.
// See docs/RASGOS-TRANSVERSALES.md for the evaluation of every C# trait.
package traits

import (
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// Named is implemented by aggregates with a human-readable name (C# INamed).
type Named interface {
	Name() vocab.Name
}

// Describable is implemented by aggregates with a description (C# IDescriptable).
type Describable interface {
	Description() vocab.Description
}

// Commentable is implemented by aggregates with a free remark (C# IComentable).
type Commentable interface {
	Remark() vocab.Remark
}

// Regulated is implemented by rules that come from a regulation and apply during a period
// (C# IRegulated + ITimeBounded).
type Regulated interface {
	RegulationSource() vocab.RegulationSource
	ValidPeriod() vocab.ValidPeriod
}
