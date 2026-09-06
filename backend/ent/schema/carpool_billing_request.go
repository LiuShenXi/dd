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

type CarpoolBillingRequest struct{ ent.Schema }

func (CarpoolBillingRequest) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "carpool_billing_requests"}}
}

func (CarpoolBillingRequest) Fields() []ent.Field {
	return []ent.Field{
		field.String("request_id").MaxLen(128),
		field.Int64("api_key_id"), field.Int64("user_id"), field.Int64("group_id"), field.Int64("term_id"), field.Int64("cycle_id"),
		field.Time("admitted_at").SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.String("status").MaxLen(32),
		field.String("request_fingerprint").MaxLen(64),
		field.JSON("billing_payload", map[string]any{}).Optional().SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
		field.Float("actual_cost_usd").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "numeric(20,8)"}),
		field.Int("retry_count").Default(0),
		field.String("last_error").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.Time("receipt_recorded_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("settled_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.String("resolution").MaxLen(32).Optional().Nillable(),
		field.Time("resolved_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Int64("resolved_by").Optional().Nillable(),
		field.String("resolution_reason").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.Time("created_at").Default(time.Now).Immutable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (CarpoolBillingRequest) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("request_id", "api_key_id").Unique(),
		index.Fields("status", "updated_at"),
		index.Fields("cycle_id", "status"),
	}
}
