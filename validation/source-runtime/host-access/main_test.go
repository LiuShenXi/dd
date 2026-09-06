package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

type fakeDockerRuntime struct {
	container containerInspect
	command   func(context.Context, string) *exec.Cmd
}

func (f *fakeDockerRuntime) inspect(context.Context, string) (containerInspect, error) {
	return f.container, nil
}

func (f *fakeDockerRuntime) execCommand(ctx context.Context, containerID string) *exec.Cmd {
	return f.command(ctx, containerID)
}

func validIdentity() runtimeIdentity {
	return runtimeIdentity{
		containerID: strings.Repeat("1", 64),
		imageID:     "sha256:" + strings.Repeat("2", 64),
		runNonce:    strings.Repeat("3", 32),
		networkID:   strings.Repeat("4", 64),
	}
}

func validContainer(identity runtimeIdentity) containerInspect {
	var container containerInspect
	container.ID = identity.containerID
	container.Name = applicationName
	container.Image = identity.imageID
	container.State.Running = true
	container.Config.Labels = map[string]string{taskLabel: runtimeTask, runLabel: identity.runNonce}
	container.HostConfig.NetworkMode = runtimeNetwork
	container.NetworkSettings.Networks = map[string]struct {
		NetworkID string `json:"NetworkID"`
	}{runtimeNetwork: {NetworkID: identity.networkID}}
	return container
}

func writeState(t *testing.T, identity runtimeIdentity) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "startup-state.json")
	state := startupState{
		Task:            runtimeTask,
		RunNonce:        identity.runNonce,
		NetworkID:       identity.networkID,
		ImageID:         identity.imageID,
		AppID:           identity.containerID,
		Healthy:         true,
		BuildComplete:   true,
		ConfigInstalled: true,
		FixturesCopied:  true,
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func helperCommand(ctx context.Context, _ string) *exec.Cmd {
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=TestBridgeHelperProcess")
	command.Env = append(os.Environ(), "CARPOOL_HOST_ACCESS_HELPER=echo")
	return command
}

func largeResponseCommand(ctx context.Context, _ string) *exec.Cmd {
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=TestBridgeHelperProcess")
	command.Env = append(os.Environ(), "CARPOOL_HOST_ACCESS_HELPER=large")
	return command
}

func TestBridgeHelperProcess(t *testing.T) {
	switch os.Getenv("CARPOOL_HOST_ACCESS_HELPER") {
	case "echo":
		_, _ = io.Copy(os.Stdout, os.Stdin)
	case "large":
		_, _ = os.Stdout.Write(bytes.Repeat([]byte("carpool-host-access\n"), 100_000))
	default:
		return
	}
	os.Exit(0)
}

func TestListenAddressIsFixedToIPv4Loopback(t *testing.T) {
	if err := validateListenAddress(listenAddress); err != nil {
		t.Fatalf("canonical loopback address rejected: %v", err)
	}
	for _, address := range []string{"0.0.0.0:38088", "[::1]:38088", "127.0.0.1:0", "localhost:38088"} {
		if err := validateListenAddress(address); err == nil {
			t.Fatalf("unsafe listen address accepted: %s", address)
		}
	}
}

func TestParseConfigRejectsUnsafeArguments(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-only command validation")
	}
	directory := t.TempDir()
	dockerPath := filepath.Join(directory, "docker.exe")
	if err := os.WriteFile(dockerPath, []byte("test"), 0o600); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(directory, "startup-state.json")
	if err := os.WriteFile(statePath, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	validArguments := []string{"-docker", dockerPath, "-state", statePath, "-max-connections", "8"}
	settings, err := parseConfig(validArguments, io.Discard)
	if err != nil || settings.maxConnections != 8 {
		t.Fatalf("valid command rejected: %#v, %v", settings, err)
	}
	unsafe := [][]string{
		{"-docker", "docker.exe", "-state", statePath},
		{"-docker", dockerPath + " & calc.exe", "-state", statePath},
		{"-docker", dockerPath, "-state", statePath, "unexpected"},
		{"-docker", dockerPath, "-state", statePath, "-max-connections", "129"},
		{"-docker", dockerPath, "-state", filepath.Join(directory, "other.json")},
	}
	for _, arguments := range unsafe {
		if _, err := parseConfig(arguments, io.Discard); err == nil {
			t.Fatalf("unsafe arguments accepted: %#v", arguments)
		}
	}
}

func TestDockerExecArgumentsCannotInvokeAShell(t *testing.T) {
	dockerPath := `C:\Program Files\Docker\docker.exe`
	client := newDockerClient(dockerPath)
	identity := validIdentity()
	command := client.execCommand(context.Background(), identity.containerID)
	expected := []string{dockerPath, "exec", "-i", identity.containerID, "/bin/busybox", "nc", "127.0.0.1", "8080"}
	if !reflect.DeepEqual(command.Args, expected) {
		t.Fatalf("unexpected Docker exec arguments: %#v", command.Args)
	}
	if runtime.GOOS == "windows" && (command.SysProcAttr == nil || !command.SysProcAttr.HideWindow) {
		t.Fatal("Docker CLI process window is not hidden on Windows")
	}
	dockerHostCount := 0
	for _, entry := range command.Env {
		if strings.HasPrefix(strings.ToUpper(entry), "DOCKER_HOST=") && entry != "DOCKER_HOST="+localDockerEndpoint {
			t.Fatalf("Docker host was not forced to the local named pipe: %q", entry)
		}
		if entry == "DOCKER_HOST="+localDockerEndpoint {
			dockerHostCount++
		}
		if strings.HasPrefix(strings.ToUpper(entry), "DOCKER_CONTEXT=") {
			t.Fatalf("inherited Docker context was retained: %q", entry)
		}
	}
	if dockerHostCount != 1 {
		t.Fatalf("expected one forced local Docker host, got %d", dockerHostCount)
	}
}

func TestContainerValidationRejectsIdentityOrNetworkChanges(t *testing.T) {
	identity := validIdentity()
	container := validContainer(identity)
	if err := validateContainer(container, identity); err != nil {
		t.Fatalf("valid application container rejected: %v", err)
	}
	container.ID = strings.Repeat("9", 64)
	if err := validateContainer(container, identity); err == nil {
		t.Fatal("replacement container was accepted")
	}
	container = validContainer(identity)
	container.NetworkSettings.Networks["unexpected"] = struct {
		NetworkID string `json:"NetworkID"`
	}{NetworkID: strings.Repeat("8", 64)}
	if err := validateContainer(container, identity); err == nil {
		t.Fatal("container with a second network was accepted")
	}
}

func TestConnectionRelaysBytesAndCleansUp(t *testing.T) {
	identity := validIdentity()
	statePath := writeState(t, identity)
	hostBridge := &bridge{
		statePath: statePath,
		expected:  identity,
		docker: &fakeDockerRuntime{
			container: validContainer(identity),
			command:   helperCommand,
		},
		maxConnections: 1,
	}
	client, server := net.Pipe()
	done := make(chan struct{})
	go func() {
		hostBridge.handleConnection(context.Background(), server)
		close(done)
	}()

	payload := []byte("GET /health HTTP/1.1\r\nHost: localhost\r\n\r\n")
	if _, err := client.Write(payload); err != nil {
		t.Fatal(err)
	}
	response := make([]byte, len(payload))
	if _, err := io.ReadFull(client, response); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(response, payload) {
		t.Fatalf("relayed bytes changed: %q", response)
	}
	_ = client.Close()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("connection process did not exit after client close")
	}
}

func TestConnectionCancellationCleansUpProcess(t *testing.T) {
	identity := validIdentity()
	hostBridge := &bridge{
		statePath: writeState(t, identity),
		expected:  identity,
		docker: &fakeDockerRuntime{
			container: validContainer(identity),
			command:   helperCommand,
		},
		maxConnections: 1,
	}
	client, server := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		hostBridge.handleConnection(ctx, server)
		close(done)
	}()
	cancel()
	defer client.Close()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("connection process did not exit after cancellation")
	}
}

func TestFiniteLargeResponseIsFullyDrainedBeforeProcessWait(t *testing.T) {
	identity := validIdentity()
	hostBridge := &bridge{
		statePath: writeState(t, identity),
		expected:  identity,
		docker: &fakeDockerRuntime{
			container: validContainer(identity),
			command:   largeResponseCommand,
		},
		maxConnections: 1,
	}
	client, server := net.Pipe()
	done := make(chan struct{})
	go func() {
		hostBridge.handleConnection(context.Background(), server)
		close(done)
	}()
	response, err := io.ReadAll(client)
	if err != nil {
		t.Fatal(err)
	}
	expected := bytes.Repeat([]byte("carpool-host-access\n"), 100_000)
	if !bytes.Equal(response, expected) {
		t.Fatalf("large response was truncated: got %d bytes, want %d", len(response), len(expected))
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("finite-response process did not exit cleanly")
	}
}

func TestConnectionLimitIsNonBlockingAndReusable(t *testing.T) {
	slots := make(chan struct{}, 1)
	if !tryAcquire(slots) {
		t.Fatal("first connection was rejected")
	}
	if tryAcquire(slots) {
		t.Fatal("connection beyond the configured limit was accepted")
	}
	<-slots
	if !tryAcquire(slots) {
		t.Fatal("released connection slot was not reusable")
	}
}
