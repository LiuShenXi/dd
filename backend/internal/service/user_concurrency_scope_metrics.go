package service

import "sync"

// These process-local counters describe only the independent Claude pool.
// Keys are built exclusively from the fixed vocabulary below, never identities.
type ClaudeConcurrencyMetricsSnapshot struct {
	Operations map[string]uint64 `json:"operations_total"`
}

var claudeConcurrencyMetrics = struct {
	sync.Mutex
	operations map[string]uint64
}{operations: make(map[string]uint64)}

func GetClaudeConcurrencyMetricsSnapshot() ClaudeConcurrencyMetricsSnapshot {
	claudeConcurrencyMetrics.Lock()
	defer claudeConcurrencyMetrics.Unlock()
	result := ClaudeConcurrencyMetricsSnapshot{Operations: make(map[string]uint64, len(claudeConcurrencyMetrics.operations))}
	for key, count := range claudeConcurrencyMetrics.operations {
		result.Operations[key] = count
	}
	return result
}

func observeClaudeScopeOperation(operation string, kind UserScopeMemberKind, result UserScopeResult, err error) {
	switch operation {
	case "acquire", "enqueue", "promote", "renew", "cleanup", "safety_deadline", "lost_lease":
	default:
		return
	}
	member := "active"
	if kind == UserScopeWaiting {
		member = "waiting"
	}
	status := "unavailable"
	switch result.Status {
	case UserScopeAcquired, UserScopeFull, UserScopeMissing:
		status = string(result.Status)
	}
	if err != nil {
		status = "error"
	}
	key := operation + "." + member + "." + status
	claudeConcurrencyMetrics.Lock()
	claudeConcurrencyMetrics.operations[key]++
	claudeConcurrencyMetrics.Unlock()
	// Lease heartbeats and safety cancellation must never wait for log I/O.
	// The recovery observer publishes a copied snapshot on its own worker.
}
