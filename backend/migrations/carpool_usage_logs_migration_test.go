package migrations

import (
	"os"
	"strings"
	"testing"
)

func TestCarpoolUsageLogsMigrationIncludesNullableAttributionAndConstraints(t *testing.T) {
	payload, err := os.ReadFile("236_carpool_usage_logs.sql")
	if err != nil {
		t.Fatalf("read carpool usage-log migration: %v", err)
	}
	sql := strings.ToLower(string(payload))
	for _, fragment := range []string{
		"add column if not exists carpool_term_id bigint",
		"add column if not exists carpool_cycle_id bigint",
		"add column if not exists carpool_admitted_at timestamptz",
		"foreign key (carpool_term_id) references carpool_terms(id) on delete set null",
		"foreign key (carpool_cycle_id) references carpool_cycles(id) on delete set null",
		"create index if not exists idx_usage_logs_carpool_term_id",
		"create index if not exists idx_usage_logs_carpool_cycle_id",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration missing %q", fragment)
		}
	}
}
