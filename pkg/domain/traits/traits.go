// Package traits ofrece los rasgos transversales componibles de los agregados de Karpo: el
// sustituto en Go de la clase base BusinessEntity de C# y sus «adjetivos» (IToggleable, IExpirable,
// IAuditable, ITesteable, INamed, IDescriptable, IComentable, IRegulated...).
//
// En lugar de una clase base que impone ocho interfaces a cada entidad, un agregado incrusta
// sólo los rasgos que necesita:
//
//	type Party struct {
//		domain.BaseAggregateRoot[PartyID]
//		traits.Activation // IsActive / Activate / Deactivate
//		traits.Validity   // periodo de vigencia, caducidad
//		traits.Audited    // creado/modificado por y en, sellado por la capa de aplicación
//		...
//	}
//
// Cada rasgo es estado más comportamiento sin efectos laterales ocultos: nunca lanza eventos ni
// lee el contexto ambiente. Los agregados deciden qué evento de dominio lanzar cuando un método de
// un rasgo informa de un cambio, y la capa de aplicación sella los datos de auditoría a partir del
// contexto de la petición. Véase docs/RASGOS-TRANSVERSALES.md para la evaluación de cada rasgo de C#.
package traits

import (
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// Named lo implementan los agregados con un nombre legible (INamed de C#).
type Named interface {
	Name() vocab.Name
}

// Describable lo implementan los agregados con una descripción (IDescriptable de C#).
type Describable interface {
	Description() vocab.Description
}

// Commentable lo implementan los agregados con una observación libre (IComentable de C#).
type Commentable interface {
	Remark() vocab.Remark
}

// Regulated lo implementan las reglas que proceden de una normativa y se aplican durante un periodo
// (IRegulated + ITimeBounded de C#).
type Regulated interface {
	RegulationSource() vocab.RegulationSource
	ValidPeriod() vocab.ValidPeriod
}
