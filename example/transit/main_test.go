package main

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/xo/transit"
)

// samples holds one short program for each grammar, and a word in it that
// the highlight query of the grammar marks with the style named beside it.
var samples = map[string]struct {
	src, word, style string
}{
	"bash":       {"for i in 1 2; do echo \"$i\"; done\n", "for", "ts-keyword"},
	"c":          {"int main(void) { return 0; }\n", "return", "ts-keyword"},
	"go":         {"package main\n\nfunc main() {\n\tprintln(\"hi\")\n}\n", "func", "ts-keyword"},
	"javascript": {"const f = function () { return 1; };\n", "return", "ts-keyword"},
	"json":       {"{\"a\": [1, true, null]}\n", "true", "ts-constant"},
	"python":     {"def f(x):\n    return x + 1\n", "def", "ts-keyword"},
	"ruby":       {"def f\n  1\nend\n", "def", "ts-keyword"},
	"rust":       {"fn main() { let x = 1; }\n", "let", "ts-keyword"},
}

// TestEveryGrammarHighlights compiles the highlight query of each grammar and
// checks that the word of its sample gets the style of its kind.
func TestEveryGrammarHighlights(t *testing.T) {
	t.Parallel()
	for name, g := range grammars {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			sample, ok := samples[name]
			if !ok {
				t.Fatalf("no sample for %s", name)
			}
			s, err := newSyntax(g)
			if err != nil {
				t.Fatal(err)
			}
			start := indexWord(sample.src, sample.word)
			end := start + len(sample.word)
			for _, sp := range s.spans(context.Background(), sample.src) {
				if sp.start == start && sp.end == end {
					if sp.style != sample.style {
						t.Errorf("%q is styled %s, expected %s", sample.word, sp.style, sample.style)
					}
					return
				}
			}
			t.Errorf("%q at %d is not styled", sample.word, start)
		})
	}
}

// TestTheLastPatternOfANodeWins checks how two captures of one node are
// settled. The highlight query of python captures every identifier as
// @variable in its first pattern, and the name of a function as @function in
// a later one. The highlighter of upstream draws the last, so the name is a
// function.
func TestTheLastPatternOfANodeWins(t *testing.T) {
	t.Parallel()
	s, err := newSyntax(grammars["python"])
	if err != nil {
		t.Fatal(err)
	}
	const src = "def f(x):\n    return x\n"
	for _, sp := range s.spans(context.Background(), src) {
		if sp.start == 4 && sp.end == 5 {
			if sp.style != "ts-function" {
				t.Errorf("the name of the function is styled %s, expected ts-function", sp.style)
			}
			return
		}
	}
	t.Error("the name of the function is not styled")
}

// TestTypingGivesTheTreeOfOneParse types each sample one character at a time,
// as rline hands the highlighter the text after each key, then deletes a
// stretch from its middle and types it again. The tree that the edits build
// must be the tree of one parse of the same text, or editBetween told the
// parser something that is not what changed.
func TestTypingGivesTheTreeOfOneParse(t *testing.T) {
	t.Parallel()
	for name, g := range grammars {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			src := samples[name].src
			s, err := newSyntax(g)
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			var texts []string
			for i := range src {
				if utf8.RuneStart(src[i]) {
					texts = append(texts, src[:i])
				}
			}
			mid := len(src) / 2
			for i := mid; i > mid/2; i-- {
				texts = append(texts, src[:i]+src[mid:])
			}
			for i := mid / 2; i <= mid; i++ {
				texts = append(texts, src[:i]+src[mid:])
			}
			for _, text := range texts {
				s.spans(ctx, text)
				want := parseOnce(t, g, text)
				if got := describe(s.tree.RootNode()); got != want {
					t.Fatalf("after the edit to %q the tree is\n%s\nand one parse gives\n%s", text, got, want)
				}
			}
		})
	}
}

// parseOnce returns the tree of one parse of text, with no old tree, as
// describe writes it.
func parseOnce(t *testing.T, g grammar, text string) string {
	t.Helper()
	p := transit.NewParser()
	if err := p.SetLanguage(g.language()); err != nil {
		t.Fatal(err)
	}
	tree, err := p.Parse(context.Background(), []byte(text), nil)
	if err != nil {
		t.Fatal(err)
	}
	return describe(tree.RootNode())
}

// describe writes every node of a tree with its range in bytes and in rows
// and columns. The form of String leaves the positions out, and a wrong row
// or column in an edit moves the nodes without changing their kinds.
func describe(root transit.Node) string {
	var b strings.Builder
	var walk func(n transit.Node, depth int)
	walk = func(n transit.Node, depth int) {
		r := n.Range()
		fmt.Fprintf(&b, "%s%s %d-%d %d:%d-%d:%d\n", strings.Repeat("  ", depth), n.Kind(),
			r.StartByte, r.EndByte, r.StartPoint.Row, r.StartPoint.Column, r.EndPoint.Row, r.EndPoint.Column)
		for c := range n.Children() {
			walk(c, depth+1)
		}
	}
	walk(root, 0)
	return b.String()
}

// TestCompletionComesFromTheGrammar checks what Tab offers where the grammar
// says what can come next. Each expected keyword is one that the grammar
// accepts there, and each refused one is a keyword that it does not.
func TestCompletionComesFromTheGrammar(t *testing.T) {
	t.Parallel()
	tests := []struct {
		lang, text   string
		want, refuse []string
	}{
		// A statement of python can start with any of these.
		{"python", "", []string{"def", "class", "import", "if", "return"}, []string{"elif", "else"}},
		// A word that is started keeps the keywords that start with it.
		{"python", "de", []string{"def", "del"}, []string{"class"}},
		// After an if and its block, elif and else can follow.
		{"python", "if x:\n    pass\nel", []string{"elif", "else"}, nil},
		// The top of a file of go is a package clause.
		{"go", "pack", []string{"package"}, nil},
		{"go", "package main\n\nfu", []string{"func"}, nil},
		{"rust", "fn main() { le", []string{"let"}, nil},
	}
	for _, test := range tests {
		t.Run(test.lang+"/"+test.text, func(t *testing.T) {
			t.Parallel()
			s, err := newSyntax(grammars[test.lang])
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			kinds := make(map[string]string)
			for _, c := range s.candidates(context.Background(), test.text, len(test.text)) {
				if c.Display == "" {
					got = append(got, c.Replacement)
					kinds[c.Replacement], _, _ = strings.Cut(c.Help, ",")
				}
			}
			for _, w := range test.want {
				switch {
				case !slices.Contains(got, w):
					t.Errorf("%q is not offered, and the offer is %q", w, got)
				case kinds[w] != "keyword":
					t.Errorf("%q is offered as a %s, and it is a keyword", w, kinds[w])
				}
			}
			for _, w := range test.refuse {
				if slices.Contains(got, w) {
					t.Errorf("%q is offered, and the grammar does not accept it there", w)
				}
			}
		})
	}
}

// TestEditBetween checks the edit that editBetween finds for a few changes,
// including two inside a character of two bytes and one across rows. The
// parser forgives a wrong row or column in most edits, so the points are
// checked here rather than left to TestTypingGivesTheTreeOfOneParse.
func TestEditBetween(t *testing.T) {
	t.Parallel()
	pt := func(row, column int) transit.Point { return transit.Point{Row: row, Column: column} }
	tests := []struct {
		old, new string
		want     transit.InputEdit
	}{
		{"abc", "abXc", transit.InputEdit{StartByte: 2, OldEndByte: 2, NewEndByte: 3,
			StartPoint: pt(0, 2), OldEndPoint: pt(0, 2), NewEndPoint: pt(0, 3)}},
		{"aa", "aaa", transit.InputEdit{StartByte: 2, OldEndByte: 2, NewEndByte: 3,
			StartPoint: pt(0, 2), OldEndPoint: pt(0, 2), NewEndPoint: pt(0, 3)}},
		{"abc", "ac", transit.InputEdit{StartByte: 1, OldEndByte: 2, NewEndByte: 1,
			StartPoint: pt(0, 1), OldEndPoint: pt(0, 2), NewEndPoint: pt(0, 1)}},
		{"é", "è", transit.InputEdit{StartByte: 0, OldEndByte: 2, NewEndByte: 2,
			StartPoint: pt(0, 0), OldEndPoint: pt(0, 2), NewEndPoint: pt(0, 2)}},
		// é is C3 A9 and ũ is C5 A9, so the two share their last byte, which
		// is inside a character.
		{"é", "ũ", transit.InputEdit{StartByte: 0, OldEndByte: 2, NewEndByte: 2,
			StartPoint: pt(0, 0), OldEndPoint: pt(0, 2), NewEndPoint: pt(0, 2)}},
		{"a\nb", "a\nbc\n", transit.InputEdit{StartByte: 3, OldEndByte: 3, NewEndByte: 5,
			StartPoint: pt(1, 1), OldEndPoint: pt(1, 1), NewEndPoint: pt(2, 0)}},
		{"a\nbc\nd", "a\nd", transit.InputEdit{StartByte: 2, OldEndByte: 5, NewEndByte: 2,
			StartPoint: pt(1, 0), OldEndPoint: pt(2, 0), NewEndPoint: pt(1, 0)}},
	}
	for _, test := range tests {
		if got := editBetween([]byte(test.old), []byte(test.new)); got != test.want {
			t.Errorf("%q to %q: the edit is\n%+v\nexpected\n%+v", test.old, test.new, got, test.want)
		}
	}
}

// indexWord returns where word first stands alone in src.
func indexWord(src, word string) int {
	for i := 0; i+len(word) <= len(src); i++ {
		if src[i:i+len(word)] != word {
			continue
		}
		before := i == 0 || !isWord(src[i-1])
		after := i+len(word) == len(src) || !isWord(src[i+len(word)])
		if before && after {
			return i
		}
	}
	return -1
}

func isWord(b byte) bool {
	return b == '_' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9'
}
