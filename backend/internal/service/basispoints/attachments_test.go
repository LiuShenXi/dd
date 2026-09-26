package basispoints

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func inlineImageFixture(t *testing.T) (string, []byte) {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 3, 2))); err != nil {
		t.Fatal(err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()), buf.Bytes()
}

func TestInlineImagesUploadAfterPrepare(t *testing.T) {
	dataURL, want := inlineImageFixture(t)
	source := testSource()
	source["input"] = []any{
		message("user", "Inspect images"),
		object{"type": "function_call", "call_id": "call_image", "name": "view_image", "arguments": "{}"},
		object{"type": "function_call_output", "call_id": "call_image", "output": []any{object{"type": "input_image", "image_url": dataURL, "detail": "high"}}},
		object{"role": "user", "content": []any{object{"type": "input_text", "text": "preserve this"}, object{"type": "input_image", "image_url": dataURL}, object{"type": "input_image", "image_url": "https://example.com/a.png?sig=unchanged"}}},
	}
	raw, _ := json.Marshal(source)
	prepared, bridge, err := Prepare(raw, "account/key/thread", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !bridge.HasInlineImages {
		t.Fatal("inline image upload was not scheduled")
	}
	var before object
	_ = decode(prepared, &before)
	var cache AttachmentCache
	uploads := 0
	upload := func(ctx context.Context, mime string, data []byte) (string, error) {
		uploads++
		if mime != "image/png" || !bytes.Equal(data, want) {
			t.Fatal("image bytes changed")
		}
		return "file-image", nil
	}
	for i := 0; i < 2; i++ {
		converted, err := cache.UploadImages(context.Background(), prepared, "account/key/thread/token", upload)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(converted, []byte(dataURL)) {
			t.Fatal("inline bytes reached inference")
		}
		var after object
		_ = decode(converted, &after)
		if !reflect.DeepEqual(before["metadata"], after["metadata"]) {
			t.Fatal("upload changed turn identity")
		}
		items := after["input"].([]any)
		parts := items[len(items)-1].(object)["content"].([]any)
		if parts[0].(object)["text"] != "preserve this" || parts[2].(object)["image_url"] != "https://example.com/a.png?sig=unchanged" {
			t.Fatal("neighboring content changed")
		}
		if !reflect.DeepEqual(parts[1], object{"type": "input_image", "file_id": "file-image", "detail": "auto"}) {
			t.Fatal("incorrect image reference")
		}
		var resultParts []any
		if err := decode([]byte(text(items[len(items)-3].(object)["output"])), &resultParts); err != nil {
			t.Fatal("tool result is not valid JSON text", err)
		}
		output := resultParts[0].(object)
		if output["file_id"] != "file-image" || output["detail"] != "high" {
			t.Fatal("tool screenshot lost")
		}
		supplement := items[len(items)-2].(object)["content"].([]any)
		if !strings.Contains(text(supplement[0].(object)["text"]), "untrusted tool output") || !reflect.DeepEqual(supplement[1], output) {
			t.Fatal("tool image or provenance was not preserved")
		}
	}
	if uploads != 1 {
		t.Fatalf("repeated images uploaded %d times", uploads)
	}
	_, err = cache.UploadImages(context.Background(), prepared, "other-account/key/thread/token", upload)
	if err != nil || uploads != 2 {
		t.Fatal("cache crossed account scope")
	}
	if !bytes.Contains(prepared, []byte(dataURL)) {
		t.Fatal("caller body mutated")
	}
}

func TestInvalidInlineImagesRejectedBeforeUpload(t *testing.T) {
	valid, _ := inlineImageFixture(t)
	for _, bad := range []string{"data:image/png;base64", "data:text/plain;base64,dGVzdA==", "data:image/png;base64,%%%", "data:image/png;base64,", "data:image/png;base64,%zz"} {
		body, _ := json.Marshal(object{"input": []any{object{"role": "user", "content": []any{object{"type": "input_image", "image_url": valid}, object{"type": "input_image", "image_url": bad}}}}})
		var cache AttachmentCache
		_, err := cache.UploadImages(context.Background(), body, "scope", func(context.Context, string, []byte) (string, error) {
			t.Fatal("invalid batch uploaded")
			return "", nil
		})
		if err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
	_, _, err := decodeInlineImage("data:image/png;base64," + strings.Repeat("A", base64.StdEncoding.EncodedLen(maxInlineImageBytes)+1))
	if err == nil {
		t.Fatal("oversized allocation accepted")
	}
}

func TestHTTPSImageToolOutputUsesCompatibleWireShape(t *testing.T) {
	source := testSource()
	source["input"] = []any{
		message("user", "Inspect"),
		object{"type": "function_call", "call_id": "call_https", "name": "view_image", "arguments": "{}"},
		object{"type": "function_call_output", "call_id": "call_https", "output": []any{object{"type": "input_text", "text": "screenshot caption"}, object{"type": "input_image", "image_url": "https://example.com/a.png"}}},
	}
	raw, _ := json.Marshal(source)
	prepared, bridge, err := Prepare(raw, "https-scope", nil)
	if err != nil || bridge.HasInlineImages || !bridge.HasToolImages {
		t.Fatal("incorrect HTTPS tool image preparation flags", err)
	}
	var cache AttachmentCache
	converted, err := cache.UploadImages(context.Background(), prepared, "https-scope", func(context.Context, string, []byte) (string, error) {
		t.Fatal("HTTPS image must not be uploaded")
		return "", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var body object
	_ = decode(converted, &body)
	items := body["input"].([]any)
	result := text(items[len(items)-2].(object)["output"])
	if !strings.Contains(result, "screenshot caption") || !strings.Contains(result, "https://example.com/a.png") {
		t.Fatal("tool result content lost")
	}
	parts := items[len(items)-1].(object)["content"].([]any)
	if parts[1].(object)["image_url"] != "https://example.com/a.png" {
		t.Fatal("tool screenshot URL changed")
	}
	source["input"] = "text only"
	raw, _ = json.Marshal(source)
	prepared, bridge, err = Prepare(raw, "scope", nil)
	if err != nil || bridge.HasInlineImages || bridge.HasToolImages {
		t.Fatal("text-only request unexpectedly needs image preparation")
	}
	converted, err = cache.UploadImages(context.Background(), prepared, "scope", nil)
	if err != nil || !bytes.Equal(prepared, converted) {
		t.Fatal("text-only request changed")
	}
}

func TestAttachmentCacheCancellationCoalescingAndFailures(t *testing.T) {
	var cache AttachmentCache
	key := sha256.Sum256([]byte("image"))
	started, release := make(chan struct{}), make(chan struct{})
	var uploads atomic.Int32
	upload := func() (string, error) {
		if uploads.Add(1) == 1 {
			close(started)
		}
		<-release
		return "file-shared", nil
	}
	var group sync.WaitGroup
	group.Add(1)
	go func() {
		defer group.Done()
		_, err := cache.getOrUpload(context.Background(), key, upload)
		if err != nil {
			t.Error(err)
		}
	}()
	<-started
	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := cache.getOrUpload(cancelCtx, key, upload); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation ignored")
	}
	for i := 0; i < 12; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			id, err := cache.getOrUpload(context.Background(), key, upload)
			if err != nil || id != "file-shared" {
				t.Error("duplicate wait failed")
			}
		}()
	}
	close(release)
	group.Wait()
	if uploads.Load() != 1 {
		t.Fatal("duplicate uploads")
	}
	failed := sha256.Sum256([]byte("failed"))
	_, err := cache.getOrUpload(context.Background(), failed, func() (string, error) { return "", errors.New("upload failed") })
	if err == nil {
		t.Fatal("upload failure lost")
	}
	id, err := cache.getOrUpload(context.Background(), failed, func() (string, error) { return "file-retry", nil })
	if err != nil || id != "file-retry" {
		t.Fatal("failure cached")
	}
}

func TestAttachmentCacheExpiryAndCapacity(t *testing.T) {
	var cache AttachmentCache
	key := sha256.Sum256([]byte("first"))
	upload := func() (string, error) { return "file-test", nil }
	_, _ = cache.getOrUpload(context.Background(), key, upload)
	entry := cache.entries[key]
	value := entry.Value.(cachedAttachment)
	value.expires = time.Now().Add(-time.Second)
	entry.Value = value
	called := false
	_, _ = cache.getOrUpload(context.Background(), key, func() (string, error) { called = true; return "file-new", nil })
	if !called {
		t.Fatal("expired file reused")
	}
	for i := 0; i < maxAttachmentCacheEntries; i++ {
		next := sha256.Sum256([]byte(fmt.Sprint(i)))
		_, _ = cache.getOrUpload(context.Background(), next, upload)
	}
	if len(cache.entries) != maxAttachmentCacheEntries || cache.entries[key] != nil || len(cache.pending) != 0 {
		t.Fatal("unbounded cache")
	}
}
