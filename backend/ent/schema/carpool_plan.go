package schema

import (
	"fmt"
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type CarpoolPlan struct{ ent.Schema }

func (CarpoolPlan) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "carpool_plans"}}
}

func (CarpoolPlan) Fields() []ent.Field {
	return []ent.Field{
		field.String("code").MaxLen(32),
		field.String("name").MaxLen(100),
		field.Float("list_price_cny").SchemaType(map[string]string{dialect.Postgres: "numeric(20,2)"}),
		field.Float("weekly_quota_usd").SchemaType(map[string]string{dialect.Postgres: "numeric(20,8)"}),
		field.Int("duration_days").Default(28),
		field.Int("cycle_days").Default(7),
		field.Float("boost_ratio").Default(0.1).Validate(func(value float64) error {
			if value != 0.1 {
				return fmt.Errorf("boost_ratio must be 0.1")
			}
			return nil
		}).SchemaType(map[string]string{dialect.Postgres: "numeric(10,8)"}),
		field.Int("boost_count").Default(3).Validate(func(value int) error {
			if value != 0 && value != 2 && value != 3 {
				return fmt.Errorf("boost_count must be 0, 2 or 3")
			}
			return nil
		}),
		field.Bool("enabled").Default(true),
		field.Int("version").Default(1),
		field.Time("created_at").Default(time.Now).Immutable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now).SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (CarpoolPlan) Indexes() []ent.Index {
	return []ent.Index{index.Fields("code", "version").Unique(), index.Fields("enabled")}
}
