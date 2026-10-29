package main

import (
	"strings"
	"testing"
)

// TestTokenize checks the style of each word in lines whose bash meaning is
// known. A word missing from want must be left unmarked.
func TestTokenize(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		text string
		want map[string]string
	}{
		{`ls -la /tmp`, map[string]string{"ls": "bash-command", "-la": "bash-option"}},
		{`echo hi`, map[string]string{"echo": "bash-builtin"}},
		{`if true; then ls; fi`, map[string]string{
			"if": "bash-keyword", "true": "bash-builtin", ";": "bash-operator",
			"then": "bash-keyword", "ls": "bash-command", "fi": "bash-keyword",
		}},
		{`for x in a b; do echo $x; done`, map[string]string{
			"for": "bash-keyword", ";": "bash-operator", "do": "bash-keyword", "echo": "bash-builtin",
			"$x": "bash-variable", "done": "bash-keyword",
		}},
		{`FOO=1 make all`, map[string]string{"FOO": "bash-variable", "make": "bash-command"}},
		{`cat < in.txt | grep -v x >> out`, map[string]string{
			"cat": "bash-command", "<": "bash-operator", "|": "bash-operator",
			"grep": "bash-command", "-v": "bash-option", ">>": "bash-operator",
		}},
		{`echo "hi ${USER}" 'no $x'`, map[string]string{
			"echo": "bash-builtin", `"hi ${USER}"`: "bash-string", "${USER}": "bash-variable", `'no $x'`: "bash-string",
		}},
		{`x=$(date +%s) # now`, map[string]string{
			"x": "bash-variable", "$(": "bash-operator", "date": "bash-command", ")": "bash-operator", "# now": "bash-comment",
		}},
		{"ls \\\n  -l\npwd", map[string]string{"ls": "bash-command", "-l": "bash-option", "pwd": "bash-builtin"}},
		{`echo a#b`, map[string]string{"echo": "bash-builtin"}},
		{`echo "a"#b`, map[string]string{"echo": "bash-builtin", `"a"`: "bash-string"}},
	} {
		got := make(map[string]string)
		for _, tok := range tokenize(tc.text) {
			got[tc.text[tok.pos:tok.pos+tok.n]] = tok.style
		}
		for text, style := range tc.want {
			if got[text] != style {
				t.Errorf("%q: %q is marked %q, want %q", tc.text, text, got[text], style)
			}
		}
		for text, style := range got {
			if _, ok := tc.want[text]; !ok {
				t.Errorf("%q: %q is marked %q, want it unmarked", tc.text, text, style)
			}
		}
	}
}

// TestIncomplete checks when the example reads another row before it hands
// the text back, which is when bash prints its PS2 prompt.
func TestIncomplete(t *testing.T) {
	t.Parallel()
	for text, want := range map[string]bool{
		"":                  false,
		"ls":                false,
		`echo "open`:        true,
		`echo 'open`:        true,
		`echo "a" 'b'`:      false,
		`echo "it's"`:       false,
		`echo 'a\'`:         false,
		"ls \\":             true,
		"ls \\\\":           false,
		"ls \\\n-l":         false,
		"echo \"a\nb\"":     false,
		`echo hi # it's`:    false,
		"echo hi # it's\nx": false,
		`echo a#'b`:         true,
	} {
		if got := incomplete(text); got != want {
			t.Errorf("incomplete(%q) = %v, want %v", strings.ReplaceAll(text, "\n", `\n`), got, want)
		}
	}
}
