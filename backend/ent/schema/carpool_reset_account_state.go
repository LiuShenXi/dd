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

type CarpoolResetAccountState struct{ ent.Schema }

func (CarpoolResetAccountState) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "carpool_reset_account_states"}}
}

func (CarpoolResetAccountState) Fields() []ent.Field {
	return []ent.Field{
		field.String("upstream_identity_hash").MaxLen(64).Unique(),
		field.Int64("representative_account_id").Optional().Nillable(),
		field.Bool("baseline_complete").Default(false),
		field.Time("last_observed_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("last_complete_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.String("health_status").MaxLen(24).Default("unknown"),
		field.Int("known_credit_count").Default(0),
		field.String("incomplete_reason").MaxLen(64).Optional().Nillable(),
		field.Int64("revision").Default(0),
		field.Time("created_at").Default(time.Now).Immutable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (CarpoolResetAccountState) Indexes() []ent.Index {
	return []ent.Index{index.Fields("health_status", "last_observed_at")}
}
