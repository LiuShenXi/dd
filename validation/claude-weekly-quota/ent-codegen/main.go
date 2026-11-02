// Run from backend/: go run ../validation/claude-weekly-quota/ent-codegen/main.go
// Generate completely in a fresh directory before replacing changed artifacts.
package main

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"entgo.io/ent/entc"
	"entgo.io/ent/entc/gen"
	"entgo.io/ent/schema/field"
)

func main() {
	if _, err := os.Stat("ent/schema/group.go"); err != nil {
		panic("run the quota generator from backend")
	}
	target, err := os.MkdirTemp("", "claude-quota-ent-")
	if err != nil {
		panic(err)
	}
	if err := entc.Generate("./ent/schema", &gen.Config{
		Target: target,
		Package: "github.com/Wei-Shaw/sub2api/ent",
		IDType: &field.TypeInfo{Type: field.TypeInt64},
		Features: []gen.Feature{gen.FeatureUpsert, gen.FeatureIntercept, gen.FeatureExecQuery, gen.FeatureLock},
	}); err != nil {
		panic(err)
	}
	changed := 0
	err = filepath.WalkDir(target, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		relative, err := filepath.Rel(target, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		destination := filepath.Join("ent", relative)
		old, _ := os.ReadFile(destination)
		if bytes.Equal(bytes.ReplaceAll(old, []byte("\r\n"), []byte("\n")), data) {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(destination, data, 0644); err != nil {
			return err
		}
		changed++
		return nil
	})
	if err != nil {
		panic(err)
	}
	fmt.Printf("completed Ent generation; %d changed artifacts; temporary source %s\n", changed, target)
}
