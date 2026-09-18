//go:build sentineldiagnostic && linux

// This executable is intentionally excluded from the production build.
package main

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"log/slog"
	"os"
	"syscall"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func main() {
	// Lower-level transports may log failures containing URLs. This one-shot
	// process discards diagnostic logs and emits only the explicit safe report.
	log.SetOutput(io.Discard)
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer func() {
		if recover() != nil {
			writeReport(service.CodexTicketDiagnosticReport{ErrorCategory: "internal_failure"})
			os.Exit(1)
		}
	}()
	path := os.Getenv("CODEX_TICKET_DIAGNOSTIC_FILE")
	if path == "" {
		writeReport(service.CodexTicketDiagnosticReport{ErrorCategory: "not_configured"})
		return
	}
	input, ok := readPrivateInput(path)
	if !ok {
		writeReport(service.CodexTicketDiagnosticReport{ErrorCategory: "invalid_private_file"})
		os.Exit(1)
	}
	cfg := &config.Config{}
	cfg.Gateway.ResponseHeaderTimeout = input.ResponseHeaderTimeoutSeconds
	cfg.Gateway.DisableCodexIdentityEnforcement = input.DisableCodexIdentityEnforcement
	cfg.Security.URLAllowlist.Enabled = input.URLAllowlistEnabled
	cfg.Security.URLAllowlist.AllowPrivateHosts = input.URLAllowlistAllowPrivateHosts
	upstream := repository.NewHTTPUpstream(cfg)
	report := service.RunCodexTicketDiagnosticProbe(context.Background(), upstream, input)
	writeReport(report)
}

func readPrivateInput(path string) (service.CodexTicketDiagnosticInput, bool) {
	var input service.CodexTicketDiagnosticInput
	if os.Geteuid() != 0 {
		return input, false
	}
	// The path is supplied by the operator, never by an HTTP request. O_NOFOLLOW
	// and descriptor-based metadata checks forbid symlinks and permissive files.
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0) // #nosec G703 -- Trusted operator env path; root ownership/private mode checked on the opened descriptor.
	if err != nil {
		return input, false
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() <= 0 || info.Size() > 32768 {
		return input, false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 {
		return input, false
	}
	decoder := json.NewDecoder(io.LimitReader(file, 32769))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF || !service.ValidCodexTicketDiagnosticInput(input) {
		return service.CodexTicketDiagnosticInput{}, false
	}
	return input, true
}

func writeReport(report service.CodexTicketDiagnosticReport) {
	_ = json.NewEncoder(os.Stdout).Encode(report)
}
