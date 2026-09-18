package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
)

var testMainOnlyImports = map[string]struct{}{
	"log": {},
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone":            {},
	"entgo.io/ent/dialect":                                         {},
	"entgo.io/ent/dialect/sql":                                     {},
	"github.com/lib/pq":                                            {},
	"github.com/testcontainers/testcontainers-go/modules/postgres": {},
	"github.com/testcontainers/testcontainers-go/modules/redis":    {},
}

type overlayFile struct {
	Replace map[string]string `json:"Replace"`
}

func main() {
	source := flag.String("source", "", "integration_harness_test.go path")
	output := flag.String("output", "", "patched harness output path")
	overlay := flag.String("overlay", "", "go overlay JSON output path")
	helper := flag.String("helper", "", "source-owned TestMain helper path")
	virtual := flag.String("virtual", "", "virtual helper path in the repository package")
	flag.Parse()

	if err := prepareOverlay(*source, *output, *overlay, *helper, *virtual); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func prepareOverlay(source, output, overlay, helper, virtual string) error {
	paths := []*string{&source, &output, &overlay, &helper, &virtual}
	for _, path := range paths {
		if *path == "" {
			return errors.New("all paths are required")
		}
		absolute, err := filepath.Abs(*path)
		if err != nil {
			return err
		}
		*path = filepath.Clean(absolute)
	}
	if filepath.Ext(helper) != ".go" || filepath.Ext(virtual) != ".go" {
		return errors.New("helper and virtual paths must be Go files")
	}
	if _, err := os.Stat(helper); err != nil {
		return fmt.Errorf("stat helper: %w", err)
	}

	fileSet := token.NewFileSet()
	parsed, err := parser.ParseFile(fileSet, source, nil, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("parse harness: %w", err)
	}
	removed := 0
	declarations := make([]ast.Decl, 0, len(parsed.Decls))
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && function.Recv == nil && function.Name.Name == "TestMain" {
			removed++
			continue
		}
		declarations = append(declarations, declaration)
	}
	if removed != 1 {
		return fmt.Errorf("expected exactly one TestMain, removed %d", removed)
	}
	parsed.Decls = declarations

	for _, declaration := range parsed.Decls {
		imports, ok := declaration.(*ast.GenDecl)
		if !ok || imports.Tok != token.IMPORT {
			continue
		}
		specifications := imports.Specs[:0]
		for _, specification := range imports.Specs {
			importSpec, ok := specification.(*ast.ImportSpec)
			if !ok {
				specifications = append(specifications, specification)
				continue
			}
			path, unquoteErr := strconv.Unquote(importSpec.Path.Value)
			if unquoteErr != nil {
				return fmt.Errorf("parse import path: %w", unquoteErr)
			}
			if _, remove := testMainOnlyImports[path]; remove {
				continue
			}
			specifications = append(specifications, specification)
		}
		imports.Specs = specifications
	}

	if err := os.MkdirAll(filepath.Dir(output), 0o700); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	outputFile, err := os.OpenFile(output, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create patched harness: %w", err)
	}
	formatErr := format.Node(outputFile, fileSet, parsed)
	closeErr := outputFile.Close()
	if formatErr != nil {
		return fmt.Errorf("format patched harness: %w", formatErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close patched harness: %w", closeErr)
	}
	if _, err := parser.ParseFile(token.NewFileSet(), output, nil, parser.AllErrors); err != nil {
		return fmt.Errorf("verify patched harness: %w", err)
	}

	configuration := overlayFile{Replace: map[string]string{
		source:  output,
		virtual: helper,
	}}
	payload, err := json.MarshalIndent(configuration, "", "  ")
	if err != nil {
		return fmt.Errorf("encode overlay: %w", err)
	}
	payload = append(payload, '\n')
	if err := os.WriteFile(overlay, payload, 0o600); err != nil {
		return fmt.Errorf("write overlay: %w", err)
	}
	return nil
}
