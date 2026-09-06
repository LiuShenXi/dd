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

type CarpoolResetBatch struct{ ent.Schema }

func (CarpoolResetBatch) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "carpool_reset_batches"}}
}

func (CarpoolResetBatch) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("scope_id"),
		field.String("status").MaxLen(24),
		field.Time("detected_at").SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("qualified_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("slot_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("scheduled_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Int("schedule_revision").Default(0),
		field.Time("effective_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("completed_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.String("delay_reason").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.JSON("evidence", map[string]any{}).Default(func() map[string]any { return map[string]any{} }).SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
		field.String("announcement_state").MaxLen(24).Default("pending"),
		field.String("qualification_source").MaxLen(24),
		field.String("source_event_key_hash").MaxLen(64),
		field.Time("created_at").Default(time.Now).Immutable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (CarpoolResetBatch) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("scope_id", "source_event_key_hash").Unique(),
		index.Fields("status", "scheduled_at"),
	}
}
