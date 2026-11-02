package basispoints

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
)

// MaxSSEEventBytes bounds the legacy buffered reader used by compatibility
// tests. Production Transform uses the bounded disk-backed reader instead.
const MaxSSEEventBytes = 64 << 20

// MaxSSELineBytes includes the data prefix, CRLF and Scanner lookahead for
// callers of the legacy buffered reader.
const MaxSSELineBytes = MaxSSEEventBytes + len("data: ") + 2

type protocolError struct{ error }

type streamBody struct {
	*io.PipeReader
	upstream io.ReadCloser
	once     sync.Once
	err      error
}

func (b *streamBody) closeUpstream() error {
	b.once.Do(func() { b.err = b.upstream.Close() })
	return b.err
}

func (b *streamBody) Close() error {
	readerErr := b.PipeReader.Close()
	return errors.Join(readerErr, b.closeUpstream())
}

// Stream keeps ordinary text incremental while withholding native tool events
// and structured final answers until validated.
// Closing the downstream body interrupts an upstream read or a blocked pipe write.
func (b *Bridge) Stream(upstream io.ReadCloser) io.ReadCloser {
	reader, writer := io.Pipe()
	body := &streamBody{PipeReader: reader, upstream: upstream}
	go func() {
		err := b.transform(upstream, writer)
		_ = body.closeUpstream()
		_ = writer.CloseWithError(err)
	}()
	return body
}

func (b *Bridge) transform(reader io.Reader, writer io.Writer) error {
	return b.Transform(reader, func(event Event) error { return event.WriteSSE(writer) })
}

// Transform delivers validated events without serializing and reparsing an
// entire event at the gateway. Callbacks must not retain spool-backed values.
func (b *Bridge) Transform(reader io.Reader, sink func(Event) error) error {
	sequence := 0
	terminal := false
	emitted := make(map[string]bool)
	pendingTools := make(map[string]bool)
	emit := func(kind string, payload object) error {
		payload["type"] = kind
		payload["sequence_number"] = sequence
		sequence++
		return sink(Event{Kind: kind, Payload: payload})
	}
	emitTool := func(item object, index any) error {
		id := text(item["id"])
		if emitted[id] {
			return nil
		}
		emitted[id] = true
		field, prefix := "arguments", "response.function_call_arguments"
		if text(item["type"]) == "custom_tool_call" {
			field, prefix = "input", "response.custom_tool_call_input"
		}
		added := make(object, len(item))
		for k, v := range item {
			added[k] = v
		}
		added[field], added["status"] = "", "in_progress"
		if err := emit("response.output_item.added", object{"output_index": index, "item": added}); err != nil {
			return err
		}
		if err := emit(prefix+".delta", object{"output_index": index, "item_id": id, "delta": item[field]}); err != nil {
			return err
		}
		if err := emit(prefix+".done", object{"output_index": index, "item_id": id, field: item[field]}); err != nil {
			return err
		}
		return emit("response.output_item.done", object{"output_index": index, "item": item})
	}
	process := func(event string, payload object) error {
		kind := text(payload["type"])
		if kind == "" {
			kind = event
		}
		if b.structured != nil && kind == "response.completed" {
			if response, ok := payload["response"].(object); !ok || response == nil {
				return fmt.Errorf("basispoints structured output is missing its terminal response")
			}
		}
		if isToolEvent(kind) {
			return nil
		}
		item, _ := payload["item"].(object)
		if b.structured != nil && isStructuredMessageEvent(kind, item) {
			return nil
		}
		if kind == "response.output_item.added" && isTool(item) {
			return nil
		}
		if kind == "response.output_item.done" && isTool(item) {
			// Only the terminal response contains the authoritative native item.
			// Text keeps streaming; tool calls wait until the whole response validates.
			if len(pendingTools) >= 1024 {
				return fmt.Errorf("basispoints response contains too many tool items")
			}
			pendingTools[text(item["call_id"])+"\x00"+text(item["id"])] = true
			return nil
		}
		if response, ok := payload["response"].(object); ok {
			if b.structured != nil {
				config, _ := response["text"].(object)
				if config == nil {
					config = make(object)
				}
				config["format"] = b.structured.format
				response["text"] = config
			}
			if kind == "response.completed" {
				// Preserve large opaque fields on disk. Only tool and structured
				// message contents need to become strings for semantic validation.
				budget := int64(16 << 20)
				largeOutput, _ := response["output"].([]any)
				for _, raw := range largeOutput {
					item, _ := raw.(object)
					if isTool(item) || (b.structured != nil && text(item["type"]) == "message") {
						if _, err := materializeEventValue(item, &budget); err != nil {
							return err
						}
					}
				}
				if b.structured != nil {
					if err := b.structured.validate(response); err != nil {
						return err
					}
				}
				output, _ := response["output"].([]any)
				for _, raw := range output {
					item, _ := raw.(object)
					if isTool(item) {
						delete(pendingTools, text(item["call_id"])+"\x00"+text(item["id"]))
					}
				}
				if len(pendingTools) != 0 {
					return fmt.Errorf("basispoints completed response omitted an original tool item")
				}
				if err := b.translateResponse(response); err != nil {
					return err
				}
				output, _ = response["output"].([]any)
				for i, raw := range output {
					item, _ := raw.(object)
					if isTool(item) {
						if err := emitTool(item, i); err != nil {
							return err
						}
					} else if b.structured != nil && text(item["type"]) == "message" {
						if err := emitStructuredMessage(item, i, emit); err != nil {
							return err
						}
					}
				}
			} else {
				// Never expose native or incomplete tool arguments to the client.
				output, _ := response["output"].([]any)
				filtered := make([]any, 0, len(output))
				for _, raw := range output {
					item, _ := raw.(object)
					if !isTool(item) && (b.structured == nil || text(item["type"]) != "message") {
						filtered = append(filtered, raw)
					}
				}
				response["output"] = filtered
				response["reasoning"] = object{"effort": b.Effort}
			}
		}
		terminal = kind == "response.completed" || kind == "response.incomplete" || kind == "response.failed" || kind == "error"
		return emit(kind, payload)
	}
	err := readSpooledEvents(reader, func(event string, payload object) error {
		if terminal {
			return io.EOF
		}
		if err := process(event, payload); err != nil {
			return protocolError{err}
		}
		if terminal {
			return io.EOF
		}
		return nil
	})
	if err != nil && !errors.Is(err, io.EOF) {
		var invalid protocolError
		if errors.Is(err, io.ErrClosedPipe) || !errors.As(err, &invalid) {
			return err
		}
		return emit("response.failed", object{"response": object{
			"status": "failed", "output": []any{},
			"error": object{"code": "basispoints_protocol_error", "message": err.Error()},
		}})
	}
	if !terminal {
		return io.ErrUnexpectedEOF
	}
	return nil
}

func readEvents(reader io.Reader, consume func(string, []byte) error) error {
	return readEventsWithLimit(reader, MaxSSEEventBytes, consume)
}

func readEventsWithLimit(reader io.Reader, maxEventBytes int, consume func(string, []byte) error) error {
	scanner := bufio.NewScanner(reader)
	maxLineBytes := maxEventBytes + len("data: ") + 2
	scanner.Buffer(make([]byte, min(64*1024, maxLineBytes)), maxLineBytes)
	var data bytes.Buffer
	hasData := false
	event := ""
	flush := func() error {
		if !hasData {
			event = ""
			return nil
		}
		err := consume(event, data.Bytes())
		// Do not retain a large event buffer for the rest of a long-lived stream.
		data = bytes.Buffer{}
		hasData = false
		event = ""
		return err
	}
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			if err := flush(); err != nil {
				return err
			}
		} else if bytes.HasPrefix(line, []byte("event:")) {
			event = strings.TrimSpace(string(line[len("event:"):]))
		} else if bytes.HasPrefix(line, []byte("data:")) {
			value := bytes.TrimPrefix(line[len("data:"):], []byte(" "))
			separator := 0
			if hasData {
				separator = 1
			}
			if len(value) > maxEventBytes-data.Len()-separator {
				return protocolError{fmt.Errorf("basispoints SSE event exceeds %d bytes", maxEventBytes)}
			}
			if hasData {
				_ = data.WriteByte('\n')
			}
			_, _ = data.Write(value)
			hasData = true
		}
	}
	if err := scanner.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			return protocolError{fmt.Errorf("basispoints SSE line exceeds %d bytes", maxLineBytes)}
		}
		return err
	}
	return flush()
}
