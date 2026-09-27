package basispoints

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/tidwall/gjson"
)

const opaquePrefix = "bps-opaque-v1:"
const opaqueThreshold = 1 << 20
const opaqueStoreLimit = 2 << 30

// OpaqueStore preserves upstream compaction ciphertext verbatim on durable disk.
// Client references are scoped to an authenticated API key. Issued files are
// never automatically evicted: losing one would invalidate a saved conversation.
type OpaqueStore struct {
	Root string
	mu   sync.Mutex
}

func (s *OpaqueStore) scopeDir(scope string) string {
	h := sha256.Sum256([]byte(scope))
	return filepath.Join(s.Root, hex.EncodeToString(h[:]))
}

func opaqueReader(v any) (io.Reader, int64, bool) {
	switch x := v.(type) {
	case *spooledJSONString:
		return io.NewSectionReader(x.file, x.offset, x.length), x.length, x.length >= opaqueThreshold
	case string:
		if len(x) < opaqueThreshold {
			return nil, 0, false
		}
		// Large production strings are spooled before reaching this method.
		return nil, 0, false
	default:
		return nil, 0, false
	}
}

func (s *OpaqueStore) save(scope string, value any) (string, error) {
	r, size, large := opaqueReader(value)
	if !large {
		return "", nil
	}
	if size > maxWireEventBytes {
		return "", fmt.Errorf("compaction record exceeds storage limit")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	dir := s.scopeDir(scope)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("compaction storage unavailable")
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, r); err != nil {
		return "", fmt.Errorf("compaction record cannot be read")
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	path := filepath.Join(dir, digest+".json")
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() || info.Size() != size {
			return "", fmt.Errorf("compaction storage integrity failure")
		}
		return opaquePrefix + digest, nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("compaction storage unavailable")
	}
	var used int64
	err := filepath.WalkDir(s.Root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type().IsRegular() {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			used += info.Size()
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("compaction storage unavailable")
	}
	if used+size > opaqueStoreLimit {
		return "", fmt.Errorf("compaction storage is full; existing conversation records remain intact")
	}
	f, err := os.CreateTemp(dir, ".pending-")
	if err != nil {
		return "", fmt.Errorf("compaction storage unavailable")
	}
	temp := f.Name()
	defer os.Remove(temp)
	defer f.Close()
	r, _, _ = opaqueReader(value)
	if n, err := io.Copy(f, r); err != nil || n != size {
		return "", fmt.Errorf("compaction storage write failed")
	}
	if err := f.Sync(); err != nil {
		return "", fmt.Errorf("compaction storage sync failed")
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("compaction storage close failed")
	}
	if err := os.Rename(temp, path); err != nil {
		return "", fmt.Errorf("compaction storage commit failed")
	}
	// Sync directory entries where the host filesystem supports it.
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return opaquePrefix + digest, nil
}

// CompactEvent changes only compaction encrypted_content. Model text, tools,
// item ids, terminal events and usage retain their original semantics.
func (s *OpaqueStore) CompactEvent(scope string, event Event, coversHistory ...bool) error {
	if s == nil || s.Root == "" {
		return nil
	}
	compact := func(item object) error {
		if text(item["type"]) != "compaction" {
			return nil
		}
		kind := event.Kind
		if kind == "" {
			kind = text(event.Payload["type"])
		}
		if len(coversHistory) > 0 && coversHistory[0] && (kind == "response.output_item.done" || kind == "response.completed") {
			if err := s.markHistoryCompaction(scope, text(item["id"])); err != nil {
				return err
			}
		}
		reference, err := s.save(scope, item["encrypted_content"])
		if err != nil {
			return err
		}
		if reference != "" {
			item["encrypted_content"] = reference
		}
		return nil
	}
	if item, ok := event.Payload["item"].(object); ok {
		if err := compact(item); err != nil {
			return err
		}
	}
	if response, ok := event.Payload["response"].(object); ok {
		output, _ := response["output"].([]any)
		for _, raw := range output {
			if item, ok := raw.(object); ok {
				if err := compact(item); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (s *OpaqueStore) historyMarker(scope, id string) string {
	h := sha256.Sum256([]byte(id))
	return filepath.Join(s.scopeDir(scope), "server-compaction-"+hex.EncodeToString(h[:])+".marker")
}

// Only the authenticated upstream streaming response can create a pruning
// marker. An arbitrary compaction in client input is not evidence that preceding
// items are redundant: standalone compact output can retain required messages.
func (s *OpaqueStore) markHistoryCompaction(scope, id string) error {
	if id == "" || len(id) > 512 {
		return nil
	}
	dir := s.scopeDir(scope)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("compaction provenance storage unavailable")
	}
	path := s.historyMarker(scope, id)
	if existing, err := os.ReadFile(path); err == nil && string(existing) == id {
		return nil
	}
	f, err := os.CreateTemp(dir, ".provenance-")
	if err != nil {
		return fmt.Errorf("compaction provenance storage unavailable")
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, err := f.WriteString(id); err != nil {
		return fmt.Errorf("compaction provenance write failed")
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("compaction provenance sync failed")
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("compaction provenance close failed")
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return fmt.Errorf("compaction provenance commit failed")
	}
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

func (s *OpaqueStore) PruneHistory(scope string, input []any) ([]any, error) {
	if s == nil || s.Root == "" {
		return input, nil
	}
	for i := len(input) - 1; i > 0; i-- {
		item, _ := input[i].(object)
		id := text(item["id"])
		if text(item["type"]) != "compaction" || id == "" || len(id) > 512 || text(item["encrypted_content"]) == "" {
			continue
		}
		marker, err := os.ReadFile(s.historyMarker(scope, id))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil || string(marker) != id {
			return nil, fmt.Errorf("compaction provenance unavailable")
		}
		return input[i:], nil
	}
	return input, nil
}

// PruneRequest applies the same verified streaming-compaction boundary to
// native HTTP fallback and /responses/compact requests. It preserves all other
// JSON fields and never re-encodes a large ciphertext string.
func (s *OpaqueStore) PruneRequest(scope string, body []byte) ([]byte, error) {
	if s == nil || s.Root == "" {
		return body, nil
	}
	input := gjson.GetBytes(body, "input")
	if !input.IsArray() {
		return body, nil
	}
	cut := 0
	var failure error
	input.ForEach(func(_, item gjson.Result) bool {
		id := item.Get("id").String()
		if item.Get("type").String() != "compaction" || id == "" || len(id) > 512 || item.Get("encrypted_content").String() == "" {
			return true
		}
		marker, err := os.ReadFile(s.historyMarker(scope, id))
		if os.IsNotExist(err) {
			return true
		}
		if err != nil || string(marker) != id {
			failure = fmt.Errorf("compaction provenance unavailable")
			return false
		}
		cut = item.Index
		return true
	})
	if failure != nil {
		return nil, failure
	}
	if cut <= input.Index+1 {
		return body, nil
	}
	pruned := make([]byte, 0, input.Index+1+len(body)-cut)
	pruned = append(pruned, body[:input.Index+1]...)
	pruned = append(pruned, body[cut:]...)
	return pruned, nil
}

var opaqueReferenceJSON = regexp.MustCompile(`"bps-opaque-v1:([0-9a-f]{64})"`)

type opaqueRequestReader struct {
	io.Reader
	files []*os.File
}

func (r *opaqueRequestReader) Close() error {
	var first error
	for _, f := range r.files {
		if err := f.Close(); first == nil {
			first = err
		}
	}
	return first
}

// OpenRequest substitutes only exact JSON string references. Raw ciphertext is
// copied from disk straight to the upstream request, without a large JSON decode
// or marshal allocation. Authentication scope is supplied by the gateway.
func (s *OpaqueStore) OpenRequest(scope string, body []byte) (io.ReadCloser, int64, error) {
	matches := opaqueReferenceJSON.FindAllSubmatchIndex(body, -1)
	if len(matches) == 0 {
		return io.NopCloser(bytes.NewReader(body)), int64(len(body)), nil
	}
	if s == nil || s.Root == "" {
		return nil, 0, fmt.Errorf("compaction storage is not configured")
	}
	if len(matches) > 64 {
		return nil, 0, fmt.Errorf("too many compaction references")
	}
	reader := &opaqueRequestReader{}
	var parts []io.Reader
	var total = int64(len(body))
	offset := 0
	for _, match := range matches {
		digest := string(body[match[2]:match[3]])
		path := filepath.Join(s.scopeDir(scope), digest+".json")
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Size() > maxWireEventBytes {
			reader.Close()
			return nil, 0, fmt.Errorf("saved compaction is unavailable for this API key")
		}
		f, err := os.Open(path)
		if err != nil {
			reader.Close()
			return nil, 0, fmt.Errorf("saved compaction is unavailable for this API key")
		}
		reader.files = append(reader.files, f)
		hash := sha256.New()
		if _, err := io.Copy(hash, f); err != nil || !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), digest) {
			reader.Close()
			return nil, 0, fmt.Errorf("saved compaction failed integrity verification")
		}
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			reader.Close()
			return nil, 0, fmt.Errorf("saved compaction cannot be read")
		}
		total += info.Size() - int64(match[1]-match[0])
		if total > maxWireEventBytes {
			reader.Close()
			return nil, 0, fmt.Errorf("restored compaction request exceeds size limit")
		}
		parts = append(parts, bytes.NewReader(body[offset:match[0]]), f)
		offset = match[1]
	}
	parts = append(parts, bytes.NewReader(body[offset:]))
	reader.Reader = io.MultiReader(parts...)
	return reader, total, nil
}
