package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type CarpoolResetTarget struct{ ent.Schema }

func (CarpoolResetTarget) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "carpool_reset_targets"}}
}

func (CarpoolResetTarget) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("batch_id"),
		field.Int64("term_id"),
		field.Int64("cycle_id"),
		field.String("status").MaxLen(24),
		field.Float("granted_usd").Default(0).SchemaType(map[string]string{dialect.Postgres: "numeric(20,8)"}),
		field.Time("executed_at").SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("created_at").Default(time.Now).Immutable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (CarpoolResetTarget) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("batch_id", "term_id").Unique(),
		index.Fields("batch_id", "cycle_id").Unique(),
		index.Fields("term_id", "executed_at"),
	}
}
