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

type CarpoolResetAnnouncementOutbox struct{ ent.Schema }

func (CarpoolResetAnnouncementOutbox) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "carpool_reset_announcement_outbox"}}
}

func (CarpoolResetAnnouncementOutbox) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("batch_id"),
		field.Int64("scope_id"),
		field.String("event_kind").MaxLen(24),
		field.Int("schedule_revision"),
		field.String("status").MaxLen(24).Default("pending"),
		field.Int64("announcement_id").Optional().Nillable(),
		field.Int64("original_announcement_id").Optional().Nillable(),
		field.String("title").MaxLen(200),
		field.String("content").SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.Int("attempts").Default(0),
		field.Time("next_attempt_at").Default(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.String("last_error").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.Time("published_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("created_at").Default(time.Now).Immutable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (CarpoolResetAnnouncementOutbox) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("batch_id", "event_kind", "schedule_revision").Unique(),
		index.Fields("status", "next_attempt_at"),
	}
}
