//go:build integration

package repository

import (
	"context"
	"log"
	"net"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	_ "github.com/Wei-Shaw/sub2api/ent/runtime"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "github.com/lib/pq"
	redisclient "github.com/redis/go-redis/v9"
)

const (
	sourceRuntimePGHost    = "carpool-integration-db"
	sourceRuntimePGPort    = "5432"
	sourceRuntimePGDB      = "sub2api_carpool_integration_20260905"
	sourceRuntimePGUser    = "carpool_integration"
	sourceRuntimeRedisHost = "carpool-integration-redis"
	sourceRuntimeRedisPort = "6379"
)

func TestMain(m *testing.M) {
	os.Exit(runSourceRuntimeTestMain(m))
}

func runSourceRuntimeTestMain(m *testing.M) int {
	if m == nil {
		log.Print("source-runtime repository harness received nil testing.M")
		return 1
	}
	for name, expected := range map[string]string{
		"CARPOOL_INTEGRATION_PG_HOST":     sourceRuntimePGHost,
		"CARPOOL_INTEGRATION_PG_PORT":     sourceRuntimePGPort,
		"CARPOOL_INTEGRATION_PG_DATABASE": sourceRuntimePGDB,
		"CARPOOL_INTEGRATION_PG_USER":     sourceRuntimePGUser,
		"CARPOOL_INTEGRATION_REDIS_HOST":  sourceRuntimeRedisHost,
		"CARPOOL_INTEGRATION_REDIS_PORT":  sourceRuntimeRedisPort,
	} {
		if strings.TrimSpace(os.Getenv(name)) != expected {
			log.Printf("source-runtime repository harness requires exact %s", name)
			return 1
		}
	}
	password := os.Getenv("CARPOOL_INTEGRATION_PG_PASSWORD")
	if strings.TrimSpace(password) == "" {
		log.Print("source-runtime repository harness requires a private PostgreSQL password")
		return 1
	}
	if strings.TrimSpace(os.Getenv("CARPOOL_INTEGRATION_RUN_ID")) == "" {
		log.Print("source-runtime repository harness requires a run ID")
		return 1
	}
	if err := timezone.Init("UTC"); err != nil {
		log.Printf("failed to initialize UTC: %v", err)
		return 1
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	dsnURL := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(sourceRuntimePGUser, password),
		Host:   net.JoinHostPort(sourceRuntimePGHost, sourceRuntimePGPort),
		Path:   sourceRuntimePGDB,
	}
	query := dsnURL.Query()
	query.Set("sslmode", "disable")
	query.Set("TimeZone", "UTC")
	dsnURL.RawQuery = query.Encode()

	var err error
	integrationDB, err = openSQLWithRetry(ctx, dsnURL.String(), 45*time.Second)
	if err != nil {
		log.Printf("failed to connect to isolated PostgreSQL: %v", err)
		return 1
	}
	defer func() { _ = integrationDB.Close() }()

	var databaseName string
	var publicTables int
	if err := integrationDB.QueryRowContext(ctx, "SELECT current_database()").Scan(&databaseName); err != nil {
		log.Printf("failed to verify isolated PostgreSQL database: %v", err)
		return 1
	}
	if databaseName != sourceRuntimePGDB {
		log.Print("isolated PostgreSQL database identity mismatch")
		return 1
	}
	if err := integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM pg_catalog.pg_tables WHERE schemaname = 'public'").Scan(&publicTables); err != nil {
		log.Printf("failed to verify empty PostgreSQL schema: %v", err)
		return 1
	}
	if publicTables != 0 {
		log.Print("isolated PostgreSQL database is not empty; refusing to run migrations")
		return 1
	}
	if err := ApplyMigrations(ctx, integrationDB); err != nil {
		log.Printf("failed to apply migrations to isolated PostgreSQL: %v", err)
		return 1
	}

	driver := entsql.OpenDB(dialect.Postgres, integrationDB)
	integrationEntClient = dbent.NewClient(dbent.Driver(driver))
	defer func() { _ = integrationEntClient.Close() }()

	integrationRedis = redisclient.NewClient(&redisclient.Options{
		Addr: net.JoinHostPort(sourceRuntimeRedisHost, sourceRuntimeRedisPort),
		DB:   0,
	})
	defer func() { _ = integrationRedis.Close() }()
	if err := integrationRedis.Ping(ctx).Err(); err != nil {
		log.Printf("failed to connect to isolated Redis: %v", err)
		return 1
	}
	redisKeys, err := integrationRedis.DBSize(ctx).Result()
	if err != nil {
		log.Printf("failed to verify isolated Redis: %v", err)
		return 1
	}
	if redisKeys != 0 {
		log.Print("isolated Redis is not empty; refusing to run repository tests")
		return 1
	}

	return m.Run()
}

func TestSourceRuntimeHarnessIsolation(t *testing.T) {
	var databaseName string
	var migrationCount int
	require.NoError(t, integrationDB.QueryRowContext(context.Background(), "SELECT current_database()").Scan(&databaseName))
	require.Equal(t, sourceRuntimePGDB, databaseName)
	require.NoError(t, integrationDB.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM schema_migrations").Scan(&migrationCount))
	require.Positive(t, migrationCount)

	client := testRedis(t)
	require.NoError(t, client.Set(context.Background(), "source-runtime-harness", "synthetic", time.Minute).Err())
	value, err := client.Get(context.Background(), "source-runtime-harness").Result()
	require.NoError(t, err)
	require.Equal(t, "synthetic", value)
}
