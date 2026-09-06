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

type CarpoolTerm struct{ ent.Schema }

func (CarpoolTerm) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "carpool_terms"}}
}

func (CarpoolTerm) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("user_id"),
		field.Int64("scope_id"),
		field.Int64("group_id"),
		field.Int64("plan_id"),
		field.JSON("plan_snapshot", map[string]any{}).SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
		field.Time("starts_at").SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("expires_at").SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.String("status").MaxLen(24),
		field.Int("boost_used").Default(0),
		field.String("source_mode").MaxLen(24).Default("new"),
		field.Bool("history_complete").Default(true),
		field.Time("statistics_since").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Int64("created_by"),
		field.String("notes").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.Time("terminated_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Int64("terminated_by").Optional().Nillable(),
		field.String("termination_reason").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.Time("created_at").Default(time.Now).Immutable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (CarpoolTerm) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "scope_id", "starts_at", "expires_at"),
		index.Fields("scope_id", "status", "starts_at", "expires_at"),
		index.Fields("group_id"),
		index.Fields("plan_id"),
	}
}
