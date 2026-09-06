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

type CarpoolOperation struct{ ent.Schema }

func (CarpoolOperation) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "carpool_operations"}}
}

func (CarpoolOperation) Fields() []ent.Field {
	return []ent.Field{
		field.String("kind").MaxLen(32),
		field.Int64("actor_id"),
		field.String("key_hash").MaxLen(64),
		field.String("request_fingerprint").MaxLen(64),
		field.String("resource_type").MaxLen(32),
		field.Int64("resource_id"),
		field.JSON("response", map[string]any{}).SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
		field.Time("created_at").Default(time.Now).Immutable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (CarpoolOperation) Indexes() []ent.Index {
	return []ent.Index{index.Fields("kind", "actor_id", "key_hash").Unique(), index.Fields("resource_type", "resource_id")}
}
