package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	listenAddress       = "127.0.0.1:38088"
	containerTargetHost = "127.0.0.1"
	containerTargetPort = "8080"
	applicationName     = "/carpool-v13-test-app"
	runtimeTask         = "09-05-sub2api-carpool-v1-3"
	runtimeNetwork      = "carpool-v13-test-net"
	taskLabel           = "com.codex.local-task"
	runLabel            = "com.codex.local-run"
	localDockerEndpoint = "npipe:////./pipe/docker_engine"
	maxStateBytes       = 64 << 10
	maxInspectBytes     = 1 << 20
	inspectTimeout      = 5 * time.Second
	processExitTimeout  = 3 * time.Second
)

var (
	hex32Pattern   = regexp.MustCompile(`^[0-9a-f]{32}$`)
	hex64Pattern   = regexp.MustCompile(`^[0-9a-f]{64}$`)
	imageIDPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

type config struct {
	dockerPath     string
	statePath      string
	maxConnections int
}

type startupState struct {
	Task            string `json:"Task"`
	RunNonce        string `json:"RunNonce"`
	NetworkID       string `json:"NetworkId"`
	ImageID         string `json:"ImageId"`
	AppID           string `json:"carpool-v13-test-app"`
	Healthy         bool   `json:"Healthy"`
	BuildComplete   bool   `json:"BuildComplete"`
	ConfigInstalled bool   `json:"ConfigInstalled"`
	FixturesCopied  bool   `json:"FixturesCopied"`
}

type runtimeIdentity struct {
	containerID string
	imageID     string
	runNonce    string
	networkID   string
}

type containerInspect struct {
	ID    string `json:"Id"`
	Name  string `json:"Name"`
	Image string `json:"Image"`
	State struct {
		Running bool `json:"Running"`
	} `json:"State"`
	Config struct {
		Labels map[string]string `json:"Labels"`
	} `json:"Config"`
	HostConfig struct {
		NetworkMode string `json:"NetworkMode"`
	} `json:"HostConfig"`
	NetworkSettings struct {
		Networks map[string]struct {
			NetworkID string `json:"NetworkID"`
		} `json:"Networks"`
	} `json:"NetworkSettings"`
}

type dockerRuntime interface {
	inspect(context.Context, string) (containerInspect, error)
	execCommand(context.Context, string) *exec.Cmd
}

type dockerClient struct {
	path string
	env  []string
}

type bridge struct {
	statePath      string
	expected       runtimeIdentity
	docker         dockerRuntime
	maxConnections int
}

type cappedBuffer struct {
	buffer    bytes.Buffer
	remaining int
	truncated bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	written := len(p)
	if len(p) > b.remaining {
		p = p[:max(b.remaining, 0)]
		b.truncated = true
	}
	_, _ = b.buffer.Write(p)
	b.remaining -= len(p)
	return written, nil
}

func parseConfig(arguments []string, output io.Writer) (config, error) {
	flags := flag.NewFlagSet("carpool-host-access", flag.ContinueOnError)
	flags.SetOutput(output)
	var result config
	flags.StringVar(&result.dockerPath, "docker", "", "absolute path to docker.exe")
	flags.StringVar(&result.statePath, "state", "", "absolute path to startup-state.json")
	flags.IntVar(&result.maxConnections, "max-connections", 32, "maximum concurrent TCP connections")
	if err := flags.Parse(arguments); err != nil {
		return config{}, err
	}
	if flags.NArg() != 0 {
		return config{}, errors.New("positional arguments are not allowed")
	}
	if runtime.GOOS != "windows" {
		return config{}, errors.New("host access is supported only on Windows")
	}
	if !filepath.IsAbs(result.dockerPath) || !strings.EqualFold(filepath.Base(result.dockerPath), "docker.exe") {
		return config{}, errors.New("docker must be an absolute docker.exe path")
	}
	dockerInfo, err := os.Stat(result.dockerPath)
	if err != nil || !dockerInfo.Mode().IsRegular() {
		return config{}, errors.New("docker.exe is unavailable")
	}
	if !filepath.IsAbs(result.statePath) || !strings.EqualFold(filepath.Base(result.statePath), "startup-state.json") {
		return config{}, errors.New("state must be an absolute startup-state.json path")
	}
	if result.maxConnections < 1 || result.maxConnections > 128 {
		return config{}, errors.New("max-connections must be between 1 and 128")
	}
	return result, nil
}

func validateListenAddress(address string) error {
	if address != listenAddress {
		return fmt.Errorf("refusing non-canonical listen address %q", address)
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil || host != "127.0.0.1" || port != "38088" {
		return errors.New("host access must bind only IPv4 loopback port 38088")
	}
	return nil
}

func readRuntimeIdentity(path string) (runtimeIdentity, error) {
	file, err := os.Open(path)
	if err != nil {
		return runtimeIdentity{}, errors.New("startup state is unavailable")
	}
	defer file.Close()

	limited := io.LimitReader(file, maxStateBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil || len(data) > maxStateBytes {
		return runtimeIdentity{}, errors.New("startup state is unreadable or oversized")
	}
	var state startupState
	if err := json.Unmarshal(data, &state); err != nil {
		return runtimeIdentity{}, errors.New("startup state is malformed")
	}
	if state.Task != runtimeTask || !hex32Pattern.MatchString(state.RunNonce) ||
		!hex64Pattern.MatchString(state.NetworkID) || !imageIDPattern.MatchString(state.ImageID) ||
		!hex64Pattern.MatchString(state.AppID) || !state.Healthy || !state.BuildComplete ||
		!state.ConfigInstalled || !state.FixturesCopied {
		return runtimeIdentity{}, errors.New("startup state is not a healthy immutable runtime")
	}
	return runtimeIdentity{
		containerID: state.AppID,
		imageID:     state.ImageID,
		runNonce:    state.RunNonce,
		networkID:   state.NetworkID,
	}, nil
}

func sameIdentity(left, right runtimeIdentity) bool {
	return left.containerID == right.containerID && left.imageID == right.imageID &&
		left.runNonce == right.runNonce && left.networkID == right.networkID
}

func newDockerClient(path string) *dockerClient {
	environment := make([]string, 0, len(os.Environ())+1)
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		switch {
		case strings.EqualFold(name, "DOCKER_HOST"), strings.EqualFold(name, "DOCKER_CONTEXT"),
			strings.EqualFold(name, "DOCKER_TLS_VERIFY"), strings.EqualFold(name, "DOCKER_CERT_PATH"):
			continue
		default:
			environment = append(environment, entry)
		}
	}
	environment = append(environment, "DOCKER_HOST="+localDockerEndpoint)
	return &dockerClient{path: path, env: environment}
}

func (d *dockerClient) command(ctx context.Context, arguments ...string) *exec.Cmd {
	command := exec.CommandContext(ctx, d.path, arguments...)
	command.Env = d.env
	hideProcessWindow(command)
	return command
}

func (d *dockerClient) inspect(ctx context.Context, containerID string) (containerInspect, error) {
	command := d.command(ctx, "inspect", "--type", "container", containerID)
	stdout := &cappedBuffer{remaining: maxInspectBytes}
	command.Stdout = stdout
	command.Stderr = io.Discard
	if err := command.Run(); err != nil || stdout.truncated {
		return containerInspect{}, errors.New("exact application container inspection failed")
	}
	var inspected []containerInspect
	if err := json.Unmarshal(stdout.buffer.Bytes(), &inspected); err != nil || len(inspected) != 1 {
		return containerInspect{}, errors.New("application container inspection was malformed")
	}
	return inspected[0], nil
}

func (d *dockerClient) execCommand(ctx context.Context, containerID string) *exec.Cmd {
	return d.command(ctx, "exec", "-i", containerID, "/bin/busybox", "nc", containerTargetHost, containerTargetPort)
}

func validateContainer(container containerInspect, expected runtimeIdentity) error {
	network, present := container.NetworkSettings.Networks[runtimeNetwork]
	if container.ID != expected.containerID || container.Name != applicationName ||
		container.Image != expected.imageID || !container.State.Running ||
		container.Config.Labels[taskLabel] != runtimeTask ||
		container.Config.Labels[runLabel] != expected.runNonce ||
		container.HostConfig.NetworkMode != runtimeNetwork || len(container.NetworkSettings.Networks) != 1 ||
		!present || network.NetworkID != expected.networkID {
		return errors.New("application container no longer matches the captured immutable runtime")
	}
	return nil
}

func (b *bridge) verify(ctx context.Context) error {
	current, err := readRuntimeIdentity(b.statePath)
	if err != nil || !sameIdentity(current, b.expected) {
		return errors.New("startup identity changed after host access launch")
	}
	inspectCtx, cancel := context.WithTimeout(ctx, inspectTimeout)
	defer cancel()
	container, err := b.docker.inspect(inspectCtx, b.expected.containerID)
	if err != nil {
		return err
	}
	return validateContainer(container, b.expected)
}

func (b *bridge) handleConnection(ctx context.Context, connection net.Conn) {
	defer connection.Close()
	if err := b.verify(ctx); err != nil {
		return
	}

	sessionCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	command := b.docker.execCommand(sessionCtx, b.expected.containerID)
	stdin, err := command.StdinPipe()
	if err != nil {
		return
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return
	}
	command.Stderr = io.Discard
	if err := command.Start(); err != nil {
		_ = stdin.Close()
		return
	}

	clientDone := make(chan struct{}, 1)
	containerDone := make(chan error, 1)
	go func() {
		_, _ = io.Copy(stdin, connection)
		_ = stdin.Close()
		clientDone <- struct{}{}
	}()
	go func() {
		_, copyErr := io.Copy(connection, stdout)
		containerDone <- copyErr
	}()

	for {
		select {
		case <-clientDone:
			clientDone = nil
		case copyErr := <-containerDone:
			_ = connection.Close()
			_ = stdin.Close()
			if copyErr != nil {
				cancel()
			}
			waitDone := make(chan struct{}, 1)
			go func() {
				_ = command.Wait()
				waitDone <- struct{}{}
			}()
			timer := time.NewTimer(processExitTimeout)
			select {
			case <-waitDone:
				timer.Stop()
			case <-timer.C:
				cancel()
				<-waitDone
			case <-ctx.Done():
				timer.Stop()
				cancel()
				<-waitDone
			}
			return
		case <-ctx.Done():
			_ = connection.Close()
			_ = stdin.Close()
			cancel()
			<-containerDone
			_ = command.Wait()
			return
		}
	}
}

func tryAcquire(slots chan struct{}) bool {
	select {
	case slots <- struct{}{}:
		return true
	default:
		return false
	}
}

func (b *bridge) serve(ctx context.Context, listener net.Listener) error {
	serveCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stopClosing := context.AfterFunc(serveCtx, func() { _ = listener.Close() })
	defer stopClosing()

	slots := make(chan struct{}, b.maxConnections)
	var connections sync.WaitGroup
	for {
		connection, err := listener.Accept()
		if err != nil {
			cancel()
			connections.Wait()
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		if !tryAcquire(slots) {
			_ = connection.Close()
			continue
		}
		connections.Add(1)
		go func() {
			defer connections.Done()
			defer func() { <-slots }()
			b.handleConnection(serveCtx, connection)
		}()
	}
}

func run() error {
	settings, err := parseConfig(os.Args[1:], io.Discard)
	if err != nil {
		return err
	}
	if err := validateListenAddress(listenAddress); err != nil {
		return err
	}
	expected, err := readRuntimeIdentity(settings.statePath)
	if err != nil {
		return err
	}
	hostBridge := &bridge{
		statePath:      settings.statePath,
		expected:       expected,
		docker:         newDockerClient(settings.dockerPath),
		maxConnections: settings.maxConnections,
	}
	startupCtx, cancelStartup := context.WithTimeout(context.Background(), inspectTimeout)
	err = hostBridge.verify(startupCtx)
	cancelStartup()
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp4", listenAddress)
	if err != nil {
		return errors.New("IPv4 loopback port 38088 is unavailable")
	}
	defer listener.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	log.Printf("host access ready on http://%s with at most %d connections", listenAddress, settings.maxConnections)
	return hostBridge.serve(ctx, listener)
}

func main() {
	if err := run(); err != nil {
		log.Printf("host access stopped: %v", err)
		os.Exit(1)
	}
}
