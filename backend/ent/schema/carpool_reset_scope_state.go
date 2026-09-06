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

type CarpoolResetScopeState struct{ ent.Schema }

func (CarpoolResetScopeState) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "carpool_reset_scope_states"}}
}

func (CarpoolResetScopeState) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("scope_id").Unique(),
		field.String("timezone").MaxLen(64).Default("Asia/Shanghai"),
		field.Time("last_successful_reset_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Int64("pending_batch_id").Optional().Nillable(),
		field.Int64("revision").Default(0),
		field.Time("created_at").Default(time.Now).Immutable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (CarpoolResetScopeState) Indexes() []ent.Index {
	return []ent.Index{index.Fields("scope_id").Unique()}
}
