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

type CarpoolLedger struct{ ent.Schema }

func (CarpoolLedger) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "carpool_ledger"}}
}

func (CarpoolLedger) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("user_id"), field.Int64("term_id"), field.Int64("cycle_id"),
		field.String("event_type").MaxLen(32),
		field.String("bucket").MaxLen(16),
		field.Float("delta_usd").SchemaType(map[string]string{dialect.Postgres: "numeric(20,8)"}),
		field.String("event_key").MaxLen(180),
		field.String("request_id").MaxLen(128).Optional().Nillable(),
		field.Int64("api_key_id").Optional().Nillable(),
		field.Int64("reset_batch_id").Optional().Nillable(),
		field.Int("boost_slot").Optional().Nillable(),
		field.Int64("actor_id").Optional().Nillable(),
		field.Int64("reverses_ledger_id").Optional().Nillable(),
		field.String("reason").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.Time("effective_at").SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("recorded_at").Default(time.Now).Immutable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (CarpoolLedger) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("event_key").Unique(),
		index.Fields("term_id", "boost_slot").Unique().Annotations(entsql.IndexWhere("boost_slot IS NOT NULL")),
		index.Fields("reverses_ledger_id").Unique().Annotations(entsql.IndexWhere("reverses_ledger_id IS NOT NULL")),
		index.Fields("user_id", "recorded_at"),
		index.Fields("term_id", "cycle_id", "recorded_at"),
		index.Fields("request_id", "api_key_id"),
	}
}
