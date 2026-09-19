package rline

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestNoTwoErrorsShareText checks the rule the collision test states but
// cannot hold: that no two Error constants have the same text.
//
// It matters because Error is a string type, so two constants with the same
// text are the same value, and errors.Is answers true for the wrong one. The
// list in TestErrorsAreConstants is written by hand, so an error added later
// and left off it is invisible — measured, not assumed: adding a fifth
// constant with the text of an existing one left the whole package green.
//
// So the constants are read out of the source rather than listed. That also
// reaches the ones this platform does not build: errUnsupported lives behind
// a build tag, and a list compiled on macOS or Linux could never mention it.
//
// The cost of reading the source is that it sees the text of a constant
// rather than its value, so a constant declared as a concatenation or as
// another constant is skipped rather than guessed at. The test says how many
// it skipped, because a rule that quietly stops covering things is the thing
// this test exists to prevent.
//
// What this does not cover, said rather than left to be found: internal/
// capture has its own Error type and its own two constants, and nothing
// holds the same rule there. It is one package with two errors that no
// caller outside the module compares against, so the argument for copying
// this machinery into it is thin — but the failure would be identical, and
// if a third error joins them that is the moment to copy it.
func TestNoTwoErrorsShareText(t *testing.T) {
	t.Parallel()
	byText := map[string]string{}
	found, skipped := 0, 0

	for _, file := range packageSource(t) {
		for _, decl := range file.file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			// A const block carries its type on the first spec that names
			// one, and later specs inherit it.
			var lastType string
			for _, spec := range gen.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				if id, ok := vs.Type.(*ast.Ident); ok {
					lastType = id.Name
				} else if vs.Type != nil {
					lastType = ""
				}
				if lastType != "Error" {
					continue
				}
				for i, ident := range vs.Names {
					if i >= len(vs.Values) {
						continue
					}
					lit, ok := vs.Values[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						skipped++
						t.Logf("%s: %s is not a plain string literal, so its text was not read",
							file.name, ident.Name)
						continue
					}
					text, err := strconv.Unquote(lit.Value)
					if err != nil {
						skipped++
						continue
					}
					found++
					if other, seen := byText[text]; seen {
						t.Errorf("%s and %s are both %q, so they are the same error: "+
							"errors.Is cannot tell them apart", other, ident.Name, text)
						continue
					}
					byText[text] = ident.Name
				}
			}
		}
	}

	// A run that found nothing would pass while checking nothing, which is
	// exactly the failure this file is about. The four that exist today are
	// the floor: ErrClosed, ErrInterrupted, errNotATerminal and the one
	// behind a build tag.
	if found < 4 {
		t.Errorf("only %d error constants were read out of the source, want at least the four "+
			"that exist: the reading is broken rather than the errors being fine", found)
	}
	t.Logf("read %d error constants, skipped %d", found, skipped)
}

// sourceFile is one parsed file of this package, with the name to report it
// by.
type sourceFile struct {
	name string
	file *ast.File
}

// packageSource parses every source file in the directory, whatever its
// build tags, which is the point: a constant this platform does not compile
// still has to hold the rules checked against it.
//
// The files are listed and parsed one at a time rather than with
// parser.ParseDir, which would do the same thing and is deprecated for doing
// it — the deprecation says it ignores build tags, and ignoring build tags is
// what is wanted here. Saying so directly is better than borrowing a
// deprecated helper for the behaviour it is deprecated for.
func packageSource(t *testing.T) []sourceFile {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("listing the package source: %v", err)
	}
	var out []sourceFile
	fset := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		// Test files are left out: an error declared in one is not part of
		// what the package offers, and a test that declared one to check
		// this rule would trip it.
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(".", name), nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		out = append(out, sourceFile{name: name, file: file})
	}
	if len(out) == 0 {
		t.Fatal("no source files were read, so this checked nothing")
	}
	return out
}
