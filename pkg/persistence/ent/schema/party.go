package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

// Party holds the schema definition for the Party entity (canonical UDM aggregate).
type Party struct {
	ent.Schema
}

// Fields of the Party.
func (Party) Fields() []ent.Field {
	return []ent.Field{
		field.String("tax_id").NotEmpty().Unique(),
		field.String("legal_name").NotEmpty(),
		field.Bool("is_active").Default(true),
	}
}

// Edges of the Party.
func (Party) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("contacts", Contact.Type),
	}
}
