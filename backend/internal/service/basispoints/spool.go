package basispoints

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"sync/atomic"
	"unicode/utf8"
)

const maxWireEventBytes = 512 << 20
const maxEventMetadataBytes = 16 << 20
const inlineJSONStringBytes = 256 << 10
const maxActiveSpoolBytes = 1 << 30

var activeSpoolBytes atomic.Int64

type spooledJSONString struct {
	file           *os.File
	offset, length int64
}

// Private temporary files live only for the synchronous event callback.
type eventSpool struct {
	metadata          bytes.Buffer
	token             bytes.Buffer
	file              *os.File
	diskBytes         int64
	size              int
	inString, escaped bool
	unicodeLeft       int
	utf               []byte
	large             bool
	start             int64
	prefix            string
	strings           map[string]*spooledJSONString
}

func (s *eventSpool) close() {
	if s.file != nil {
		name := s.file.Name()
		_ = s.file.Close()
		_ = os.Remove(name)
		activeSpoolBytes.Add(-s.diskBytes)
		s.file = nil
		s.diskBytes = 0
	}
}

func (s *eventSpool) appendMeta(p []byte) error {
	if len(p) > maxEventMetadataBytes-s.metadata.Len() {
		return fmt.Errorf("basispoints SSE metadata exceeds %d bytes", maxEventMetadataBytes)
	}
	_, err := s.metadata.Write(p)
	return err
}

func (s *eventSpool) spill(p []byte) error {
	if s.file == nil {
		f, err := os.CreateTemp("", "sub2api-bps-event-*")
		if err != nil {
			return fmt.Errorf("basispoints event spool unavailable")
		}
		s.file = f
		// Unix can unlink an open file: a crash then cannot leave opaque context
		// on disk. Windows keeps the private file until close() removes it.
		_ = os.Remove(f.Name())
	}
	for {
		old := activeSpoolBytes.Load()
		if int64(len(p)) > maxActiveSpoolBytes-old {
			return fmt.Errorf("basispoints active event spool budget exceeded")
		}
		if activeSpoolBytes.CompareAndSwap(old, old+int64(len(p))) {
			break
		}
	}
	n, err := s.file.Write(p)
	s.diskBytes += int64(n)
	activeSpoolBytes.Add(-int64(len(p) - n))
	if err != nil || n != len(p) {
		return fmt.Errorf("basispoints event spool write failed")
	}
	return nil
}

// Validate string contents before replacing them in compact metadata. The
// standard decoder validates the entire compacted JSON grammar afterward.
func (s *eventSpool) stringByte(c byte) (bool, error) {
	if len(s.utf) > 0 {
		s.utf = append(s.utf, c)
		if utf8.FullRune(s.utf) {
			r, n := utf8.DecodeRune(s.utf)
			if r == utf8.RuneError && n == 1 {
				return false, fmt.Errorf("invalid UTF-8 in basispoints event")
			}
			s.utf = nil
		}
		return false, nil
	}
	if s.unicodeLeft > 0 {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false, fmt.Errorf("invalid JSON escape in basispoints event")
		}
		s.unicodeLeft--
		return false, nil
	}
	if s.escaped {
		s.escaped = false
		switch c {
		case 'u':
			s.unicodeLeft = 4
		case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
		default:
			return false, fmt.Errorf("invalid JSON escape in basispoints event")
		}
		return false, nil
	}
	switch {
	case c == '"':
		return true, nil
	case c == '\\':
		s.escaped = true
	case c < 0x20:
		return false, fmt.Errorf("invalid JSON string in basispoints event")
	case c >= utf8.RuneSelf:
		s.utf = append(s.utf, c)
		if !utf8.RuneStart(c) {
			return false, fmt.Errorf("invalid UTF-8 in basispoints event")
		}
	}
	return false, nil
}

func (s *eventSpool) write(p []byte) error {
	if len(p) > maxWireEventBytes-s.size {
		return fmt.Errorf("basispoints SSE event exceeds disk-backed budget of %d bytes", maxWireEventBytes)
	}
	s.size += len(p)
	for _, c := range p {
		if !s.inString {
			if c == '"' {
				s.inString = true
				s.token.Reset()
				s.token.WriteByte(c)
				s.large = false
				s.start = s.diskBytes
			} else if err := s.appendMeta([]byte{c}); err != nil {
				return err
			}
			continue
		}
		end, err := s.stringByte(c)
		if err != nil {
			return err
		}
		s.token.WriteByte(c)
		if s.token.Len() >= inlineJSONStringBytes {
			if err := s.spill(s.token.Bytes()); err != nil {
				return err
			}
			s.token.Reset()
			s.large = true
		}
		if end {
			if s.large {
				if err := s.spill(s.token.Bytes()); err != nil {
					return err
				}
				s.token.Reset()
				if s.prefix == "" {
					var nonce [16]byte
					if _, err := rand.Read(nonce[:]); err != nil {
						return err
					}
					s.prefix = "__bps_spooled_" + hex.EncodeToString(nonce[:]) + "_"
					s.strings = make(map[string]*spooledJSONString)
				}
				key := s.prefix + strconv.Itoa(len(s.strings))
				s.strings[key] = &spooledJSONString{file: s.file, offset: s.start, length: s.diskBytes - s.start}
				encoded, _ := json.Marshal(key)
				if err := s.appendMeta(encoded); err != nil {
					return err
				}
			} else if err := s.appendMeta(s.token.Bytes()); err != nil {
				return err
			}
			s.token.Reset()
			s.inString = false
		}
	}
	return nil
}

func (s *eventSpool) payload() (object, error) {
	if s.inString {
		return nil, fmt.Errorf("unterminated JSON string in basispoints event")
	}
	var result object
	if err := decode(s.metadata.Bytes(), &result); err != nil || result == nil {
		return nil, fmt.Errorf("invalid Basispoints SSE event")
	}
	var restore func(any) (any, error)
	restore = func(v any) (any, error) {
		switch x := v.(type) {
		case string:
			if b := s.strings[x]; b != nil {
				return b, nil
			}
		case object:
			for k, value := range x {
				if s.strings[k] != nil {
					return nil, fmt.Errorf("basispoints JSON field name exceeds supported size")
				}
				r, err := restore(value)
				if err != nil {
					return nil, err
				}
				x[k] = r
			}
		case []any:
			for i, value := range x {
				r, err := restore(value)
				if err != nil {
					return nil, err
				}
				x[i] = r
			}
		}
		return v, nil
	}
	_, err := restore(result)
	return result, err
}

// Only tool and structured-answer fields need materialization for validation.
func materializeEventValue(v any, remaining *int64) (any, error) {
	switch x := v.(type) {
	case *spooledJSONString:
		if x.length > *remaining {
			return nil, fmt.Errorf("basispoints tool or structured value exceeds 16 MiB")
		}
		*remaining -= x.length
		raw := make([]byte, int(x.length))
		if _, err := x.file.ReadAt(raw, x.offset); err != nil {
			return nil, err
		}
		var str string
		if err := json.Unmarshal(raw, &str); err != nil {
			return nil, err
		}
		return str, nil
	case object:
		for k, value := range x {
			r, err := materializeEventValue(value, remaining)
			if err != nil {
				return nil, err
			}
			x[k] = r
		}
	case []any:
		for i, value := range x {
			r, err := materializeEventValue(value, remaining)
			if err != nil {
				return nil, err
			}
			x[i] = r
		}
	}
	return v, nil
}

// Do not marshal complete large events back into contiguous byte slices.
func writeEventJSON(w io.Writer, v any) error {
	switch x := v.(type) {
	case *spooledJSONString:
		_, err := io.CopyN(w, io.NewSectionReader(x.file, x.offset, x.length), x.length)
		return err
	case object:
		if _, err := io.WriteString(w, "{"); err != nil {
			return err
		}
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for i, k := range keys {
			if i > 0 {
				if _, err := io.WriteString(w, ","); err != nil {
					return err
				}
			}
			raw, _ := json.Marshal(k)
			if _, err := w.Write(raw); err != nil {
				return err
			}
			if _, err := io.WriteString(w, ":"); err != nil {
				return err
			}
			if err := writeEventJSON(w, x[k]); err != nil {
				return err
			}
		}
		_, err := io.WriteString(w, "}")
		return err
	case []any:
		if _, err := io.WriteString(w, "["); err != nil {
			return err
		}
		for i, value := range x {
			if i > 0 {
				if _, err := io.WriteString(w, ","); err != nil {
					return err
				}
			}
			if err := writeEventJSON(w, value); err != nil {
				return err
			}
		}
		_, err := io.WriteString(w, "]")
		return err
	default:
		raw, err := json.Marshal(v)
		if err != nil {
			return err
		}
		_, err = w.Write(raw)
		return err
	}
}

// Event and its spool references remain valid until the callback returns.
type Event struct {
	Kind    string
	Payload map[string]any
}

func (e Event) WriteSSE(w io.Writer) error {
	b := bufio.NewWriterSize(w, 64<<10)
	if _, err := io.WriteString(b, "event: "+e.Kind+"\ndata: "); err != nil {
		return err
	}
	if err := writeEventJSON(b, e.Payload); err != nil {
		return err
	}
	if _, err := io.WriteString(b, "\n\n"); err != nil {
		return err
	}
	return b.Flush()
}
func (e Event) WriteResponse(w io.Writer) error {
	b := bufio.NewWriterSize(w, 64<<10)
	if err := writeEventJSON(b, e.Payload["response"]); err != nil {
		return err
	}
	return b.Flush()
}
func (e Event) ResponseID() string { r, _ := e.Payload["response"].(object); return text(r["id"]) }
func (e Event) ResponseModel() string {
	r, _ := e.Payload["response"].(object)
	return text(r["model"])
}
func (e Event) UsageJSON() []byte {
	r, _ := e.Payload["response"].(object)
	raw, _ := json.Marshal(object{"type": e.Kind, "usage": e.Payload["usage"], "response": object{"usage": r["usage"]}})
	return raw
}

func readSpooledEvents(reader io.Reader, consume func(string, object) error) error {
	r := bufio.NewReaderSize(reader, 64<<10)
	s := &eventSpool{}
	defer func() { s.close() }()
	event := ""
	hasData := false
	lineStart := true
	lineKind := ""
	firstData := false
	var header []byte
	var pendingCR bool
	flush := func() error {
		defer func() { s.close(); s = &eventSpool{}; event = ""; hasData = false }()
		if !hasData {
			return nil
		}
		if bytes.Equal(bytes.TrimSpace(s.metadata.Bytes()), []byte("[DONE]")) {
			return nil
		}
		payload, err := s.payload()
		if err != nil {
			return protocolError{err}
		}
		return consume(event, payload)
	}
	for {
		fragment, readErr := r.ReadSlice('\n')
		endLine := len(fragment) > 0 && fragment[len(fragment)-1] == '\n'
		if endLine {
			fragment = fragment[:len(fragment)-1]
		}
		if lineStart {
			if endLine && (len(fragment) == 0 || bytes.Equal(fragment, []byte{'\r'})) {
				if err := flush(); err != nil {
					return err
				}
				continue
			}
			switch {
			case bytes.HasPrefix(fragment, []byte("data:")):
				lineKind = "data"
				fragment = fragment[5:]
				firstData = true
				if hasData {
					if err := s.write([]byte{'\n'}); err != nil {
						return protocolError{err}
					}
				}
				hasData = true
			case bytes.HasPrefix(fragment, []byte("event:")):
				lineKind = "event"
				fragment = fragment[6:]
				header = nil
			default:
				lineKind = "ignore"
			}
			lineStart = false
		}
		if lineKind == "data" {
			if firstData && len(fragment) > 0 {
				if fragment[0] == ' ' {
					fragment = fragment[1:]
				}
				firstData = false
			}
			if pendingCR {
				if len(fragment) > 0 || !endLine {
					if err := s.write([]byte{'\r'}); err != nil {
						return protocolError{err}
					}
				}
				pendingCR = false
			}
			if len(fragment) > 0 && fragment[len(fragment)-1] == '\r' {
				fragment = fragment[:len(fragment)-1]
				pendingCR = !endLine
			}
			if err := s.write(fragment); err != nil {
				return protocolError{err}
			}
		} else if lineKind == "event" {
			if len(fragment) > 1024-len(header) {
				return protocolError{fmt.Errorf("basispoints SSE event name exceeds 1024 bytes")}
			}
			header = append(header, fragment...)
			if endLine || readErr == io.EOF {
				event = string(bytes.TrimSpace(header))
			}
		}
		if endLine {
			lineStart = true
			pendingCR = false
		}
		if readErr != nil && readErr != bufio.ErrBufferFull {
			if readErr != io.EOF {
				return readErr
			}
			return flush()
		}
	}
}
