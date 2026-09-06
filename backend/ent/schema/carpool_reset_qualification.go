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

type CarpoolResetQualification struct{ ent.Schema }

func (CarpoolResetQualification) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "carpool_reset_qualifications"}}
}

func (CarpoolResetQualification) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("scope_id"),
		field.Int64("batch_id"),
		field.String("source").MaxLen(24),
		field.String("source_event_key_hash").MaxLen(64),
		field.String("reason").SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.Time("confirmed_at").SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("created_at").Default(time.Now).Immutable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (CarpoolResetQualification) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("scope_id", "source_event_key_hash").Unique(),
		index.Fields("batch_id"),
	}
}
