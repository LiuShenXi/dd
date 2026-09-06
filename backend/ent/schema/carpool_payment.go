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

type CarpoolPayment struct{ ent.Schema }

func (CarpoolPayment) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "carpool_payments"}}
}

func (CarpoolPayment) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("term_id"),
		field.Float("amount_cny").SchemaType(map[string]string{dialect.Postgres: "numeric(20,2)"}),
		field.String("payment_kind").MaxLen(16),
		field.Time("paid_at").SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.String("channel").MaxLen(64),
		field.String("external_order_no").MaxLen(128).Optional().Nillable(),
		field.String("request_id").MaxLen(128),
		field.String("request_fingerprint").MaxLen(64),
		field.Int64("recorded_by"),
		field.String("notes").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.Time("recorded_at").Default(time.Now).Immutable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (CarpoolPayment) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("term_id", "request_id").Unique(),
		index.Fields("term_id", "paid_at"),
		index.Fields("external_order_no"),
	}
}
