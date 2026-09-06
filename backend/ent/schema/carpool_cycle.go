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

type CarpoolCycle struct{ ent.Schema }

func (CarpoolCycle) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "carpool_cycles"}}
}

func (CarpoolCycle) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("term_id"),
		field.Int("cycle_no"),
		field.Time("starts_at").SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("ends_at").SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Float("base_quota_usd").SchemaType(map[string]string{dialect.Postgres: "numeric(20,8)"}),
		field.Float("base_balance_usd").Default(0).SchemaType(map[string]string{dialect.Postgres: "numeric(20,8)"}),
		field.Float("boost_balance_usd").Default(0).SchemaType(map[string]string{dialect.Postgres: "numeric(20,8)"}),
		field.Float("manual_balance_usd").Default(0).SchemaType(map[string]string{dialect.Postgres: "numeric(20,8)"}),
		field.String("state").MaxLen(24).Default("scheduled"),
		field.Int64("revision").Default(0),
		field.Time("activated_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("closed_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("created_at").Default(time.Now).Immutable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (CarpoolCycle) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("term_id", "cycle_no").Unique(),
		index.Fields("state", "starts_at", "ends_at"),
	}
}
