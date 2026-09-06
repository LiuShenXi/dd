# Existing Service Failure Baseline

This diagnostic confirms existing failures on Windows. It does not mark the current full service suite green.

## Baseline Isolation

- Exact commit: `36266f512776d78d4f1645a75ae0e84816f8a0a7`.
- Generated with `git archive --format=tar <commit> backend`, then extracted into a new task-specific Windows temporary directory. No carpool worktree changes or copied-runtime data were included.
- Toolchain: `C:/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.27.0.windows-amd64/bin/go.exe`.
- Command from extracted `backend`: `go test -tags=unit ./internal/service -run '^(TestContentModerationRuntimeSnapshotRefreshFailureKeepsStaleConfig|TestPluginPackageInstallerInstallUnsignedDevelopmentPackage|TestPluginPackageInstallerAllowsRepeatedIdenticalUpload|TestPluginPackageInstallerVerifiesTrustedSignature|TestPluginPackageInstallerKeepsHostVersionMismatchDisabled)$' -count=3`.
- Exit code: 1. Actual stdout/stderr: `director-baseline-service-five-tests.log`.

## Actual Results

Each of the four plugin installer tests failed in all three iterations. The assertion fails in `plugin_package_test.go` while renaming an uploaded plugin archive; Windows reports that the file is being used by another process. The moderation test failed once in the three iterations at `content_moderation_runtime_cache_test.go:279` with `Condition never satisfied`.

These are the same five named failures observed in the current full service suite. The baseline reproduction now supports describing them as pre-existing Windows/test-environment failures, while preserving the non-green full-suite result. The task does not modify these unrelated implementation or test files.
