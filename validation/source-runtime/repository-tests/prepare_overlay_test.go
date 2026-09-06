package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPrepareOverlayRemovesOnlyTestMainAndDedicatedImports(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "integration_harness_test.go")
	helper := filepath.Join(directory, "source_runtime_testmain_test.go")
	output := filepath.Join(directory, "patched.go")
	overlay := filepath.Join(directory, "overlay.json")
	virtual := filepath.Join(directory, "virtual_testmain_test.go")

	require.NoError(t, os.WriteFile(source, []byte(`//go:build integration

package repository

import (
    "context"
    "log"
    "testing"
    "github.com/stretchr/testify/require"
    tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestMain(m *testing.M) { log.Print(tcpostgres.BasicWaitStrategies()); m.Run() }
func preserved(t *testing.T) { require.NotNil(t, context.Background()) }
`), 0o600))
	require.NoError(t, os.WriteFile(helper, []byte("package repository\n"), 0o600))

	require.NoError(t, prepareOverlay(source, output, overlay, helper, virtual))
	patched, err := os.ReadFile(output)
	require.NoError(t, err)
	require.NotContains(t, string(patched), "func TestMain")
	require.NotContains(t, string(patched), "testcontainers")
	require.NotContains(t, string(patched), `"log"`)
	require.Contains(t, string(patched), "func preserved")
	require.Contains(t, string(patched), "github.com/stretchr/testify/require")

	var configuration overlayFile
	payload, err := os.ReadFile(overlay)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(payload, &configuration))
	require.Equal(t, output, configuration.Replace[source])
	require.Equal(t, helper, configuration.Replace[virtual])
	require.False(t, strings.Contains(string(payload), "postgres.env"))
}
