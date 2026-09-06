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

type CarpoolResetCredit struct{ ent.Schema }

func (CarpoolResetCredit) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "carpool_reset_credits"}}
}

func (CarpoolResetCredit) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("account_state_id"),
		field.String("upstream_identity_hash").MaxLen(64),
		field.String("credit_hash").MaxLen(64),
		field.Time("first_seen_at").SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("last_seen_at").SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("expires_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Bool("initial_stock").Default(false),
		field.String("assignment_status").MaxLen(24).Default("pending"),
		field.Int64("reset_batch_id").Optional().Nillable(),
		field.Time("created_at").Default(time.Now).Immutable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (CarpoolResetCredit) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("upstream_identity_hash", "credit_hash").Unique(),
		index.Fields("assignment_status", "first_seen_at"),
		index.Fields("reset_batch_id"),
	}
}
