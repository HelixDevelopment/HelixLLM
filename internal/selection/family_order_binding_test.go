package selection

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/internal/catalogue"
)

// EX-9, structurally.
//
// TestFamilyOrderCoversEveryRecordedFamily already binds familyOrder to a list
// of families — but that list is written out BY HAND in the test. That is the
// same shape as the defect: two lists that must agree, kept in step by someone
// remembering. When video-generation and audio-classification were added, the
// person who added them updated neither familyOrder nor any test list, and the
// only symptom was an absence: a request naming no families enumerated neither,
// with no error and no empty family carrying a reason.
//
// This closes the remaining half by deriving the expected set from the
// DECLARATION SITE — the `Family... CapabilityFamily = "..."` constants in
// catalogue/entry.go. A new family cannot be declared without appearing here, so
// forgetting familyOrder is caught by the act of declaring the constant rather
// than by remembering to update a test.
//
// It reads the constants' string VALUES, not their Go names, because the value
// is what the data files carry and what familyRank compares.
func TestFamilyOrderIsBoundToTheDeclaredFamilyConstants(t *testing.T) {
	declared := declaredFamilyConstants(t)
	if len(declared) < 2 {
		t.Fatalf("found %d declared family constants; the guard would prove nothing", len(declared))
	}

	inOrder := map[catalogue.CapabilityFamily]bool{}
	for _, f := range familyOrder {
		inOrder[f] = true
	}

	for name, value := range declared {
		if !inOrder[value] {
			t.Errorf("catalogue declares %s = %q but familyOrder does not list it: a request "+
				"naming no families will never enumerate that family. Nothing errors — entries "+
				"in it simply never appear, which is the one outcome the per-family guarantee "+
				"exists to prevent (EX-9).", name, value)
		}
	}

	// The reverse direction: an entry in familyOrder that the catalogue no longer
	// declares would enumerate a family that can never have members.
	declaredValues := map[catalogue.CapabilityFamily]bool{}
	for _, v := range declared {
		declaredValues[v] = true
	}
	for _, f := range familyOrder {
		if !declaredValues[f] {
			t.Errorf("familyOrder lists %q, which the catalogue no longer declares as a "+
				"CapabilityFamily constant", f)
		}
	}

	t.Logf("familyOrder is bound to %d declared family constants", len(declared))
}

// declaredFamilyConstants parses catalogue/entry.go and returns every
// `Family<X> CapabilityFamily = "<value>"` constant as name -> value.
//
// Parsing the declaration is deliberate: importing a list the catalogue exports
// would only move the hand-maintained list one package over, and the whole point
// is that no human step stands between declaring a family and this test seeing it.
func declaredFamilyConstants(t *testing.T) map[string]catalogue.CapabilityFamily {
	t.Helper()

	path := filepath.Join("..", "catalogue", "entry.go")
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	out := map[string]catalogue.CapabilityFamily{}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			// Only constants explicitly typed CapabilityFamily. An untyped
			// string constant is not a family, and a constant in an
			// iota-style group inherits its type from a previous line — which
			// this deliberately does not follow, because a family is always
			// declared with its literal value here.
			ident, ok := vs.Type.(*ast.Ident)
			if !ok || ident.Name != "CapabilityFamily" {
				continue
			}
			for i, name := range vs.Names {
				if i >= len(vs.Values) {
					continue
				}
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				// Strip the surrounding quotes without unquoting escapes;
				// family values are plain lowercase identifiers.
				value := lit.Value[1 : len(lit.Value)-1]
				out[name.Name] = catalogue.CapabilityFamily(value)
			}
		}
	}
	return out
}
