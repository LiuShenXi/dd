//go:build integration

package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Keep the original Windows invocation while allowing the same synthetic
// image tests to run against the developer's selected local Docker context.
func subscriptionQuotaDockerCommand(ctx context.Context, args ...string) *exec.Cmd {
	if len(args) > 1 && args[0] == "run" {
		image := args[len(args)-1]
		hasPlatform := false
		for _, arg := range args {
			if arg == "--platform" || strings.HasPrefix(arg, "--platform=") {
				hasPlatform = true
			}
		}
		if !hasPlatform && (strings.HasPrefix(image, "sub2api:") || strings.HasPrefix(image, "claude-weekly-quota-provider:")) {
			args = append([]string{"run", "--platform", "linux/amd64"}, args[1:]...)
		}
	}
	if runtime.GOOS == "windows" {
		binary := "C:/Program Files/Docker/Docker/resources/bin/docker.exe"
		if _, err := os.Stat(binary); err == nil {
			return exec.CommandContext(ctx, binary, append([]string{"--host", "npipe:////./pipe/docker_engine"}, args...)...)
		}
	}
	return exec.CommandContext(ctx, "docker", args...)
}

// Docker may emit successful platform warnings on stderr. Do not concatenate
// them into machine-readable container IDs, JSON or SQL result cells.
func subscriptionQuotaDockerOutput(ctx context.Context, input io.Reader, args ...string) ([]byte, error) {
	cmd := subscriptionQuotaDockerCommand(ctx, args...)
	cmd.Stdin = input
	var diagnostics bytes.Buffer
	cmd.Stderr = &diagnostics
	out, err := cmd.Output()
	if err != nil || len(args) > 0 && args[0] == "logs" {
		out = append(out, diagnostics.Bytes()...)
	}
	return out, err
}

func subscriptionQuotaPrivateRoot(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		base := os.Getenv("LOCALAPPDATA")
		require.NotEmpty(t, base)
		return filepath.Join(base, "Codex", "PrivateTests", "claude-weekly-quota")
	}
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	return filepath.Join(home, ".codex", "private", "claude-weekly-quota")
}

func subscriptionQuotaStartBridge(t *testing.T, e *subscriptionQuotaI4Environment, container string, port int, private string) int {
	t.Helper()
	if runtime.GOOS == "windows" {
		cmd := exec.Command(os.Getenv("CWQ_I4_PWSH"), "-NoProfile", "-NonInteractive", "-File", os.Getenv("CWQ_I4_BRIDGE_SCRIPT"), "-ContainerID", container, "-Port", strconv.Itoa(port), "-PrivateDir", private)
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "start isolated bridge: %s", e.redact(output))
		pid, err := strconv.Atoi(strings.TrimSpace(string(output)))
		require.NoError(t, err)
		return pid
	}
	// The byte bridge never logs payloads, publishes container ports or grants
	// the isolated network external egress. Verify the exact target before launch.
	require.Len(t, container, 64)
	require.Equal(t, "claude-weekly-quota", strings.TrimSpace(string(e.docker(t, nil, "inspect", "--format", "{{index .Config.Labels \"com.codex.local-task\"}}", container))))
	name := strings.TrimSpace(string(e.docker(t, nil, "inspect", "--format", "{{.Name}}", container)))
	require.True(t, name == "/"+e.RunID+"-candidate" || name == "/"+e.RunID+"-candidate-2")
	binary := os.Getenv("CWQ_LOOPBACK_BRIDGE")
	require.NotEmpty(t, binary, "compile the repository's loopback bridge and set CWQ_LOOPBACK_BRIDGE")
	metadata := filepath.Join(private, "loopback-bridge-"+strconv.Itoa(port)+".log")
	cmd := exec.Command(binary, "--container", container, "--port", strconv.Itoa(port), "--log", metadata)
	require.NoError(t, cmd.Start())
	pid := cmd.Process.Pid
	ready := false
	defer func() {
		if !ready {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Process.Release()
	}()
	require.Eventually(t, func() bool {
		conn, err := net.DialTimeout("tcp4", "127.0.0.1:"+strconv.Itoa(port), 100*time.Millisecond)
		if err != nil {
			return false
		}
		_ = conn.Close()
		return true
	}, 5*time.Second, 50*time.Millisecond)
	ready = true
	receipt, err := json.MarshalIndent(map[string]any{"pid": pid, "container": container, "listen": "127.0.0.1", "port": port, "payload_logging": false, "external_network": false}, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(private, "loopback-bridge-"+strconv.Itoa(port)+"-receipt.json"), receipt, 0600))
	return pid
}
