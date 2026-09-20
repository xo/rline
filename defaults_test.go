package rline

import (
	"bytes"
	"testing"
)

// TestEverySwitchDefaultsOn checks that a session built with no options has
// every feature turned on.
//
// The switches are positive — color rather than noColor — so a default of on
// has to be written down rather than falling out of the zero value. Nothing
// checks polarity: a switch left false in the defaults, or right there and
// never copied onward, compiles clean and quietly turns a feature off. That
// is a silent wrong answer rather than a loud one, which is the reason to
// name every switch here rather than trust that thirteen hand flips all
// landed.
//
// It walks both hops, because a switch can be wrong in either and the second
// is invisible from the option that sets it: New's defaults, and the copy
// into env and into the terminal.
func TestEverySwitchDefaultsOn(t *testing.T) {
	restore := saveEnv(t)
	defer restore()
	setTermEnv("", "xterm-256color", "")

	// A bytes.Buffer counts as a terminal, because a caller that passes its
	// own writer has said where the output goes. So colour stays on and this
	// reaches the same defaults a real session gets.
	var out bytes.Buffer
	p, err := New(WithStdout(&out))
	if err != nil {
		t.Fatalf("building a session with no options: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })

	for _, test := range []struct {
		name string
		got  bool
	}{
		// The editor's own switches, which live on env.
		{"multiline", p.env.multiline},
		{"multilineIndent", p.env.multilineIndent},
		{"braceMatching", p.env.braceMatching},
		{"hints", p.env.hints},
		{"inlineHelp", p.env.inlineHelp},
		{"completePreview", p.env.completePreview},
		// The terminal's, which take a different route and so can be missed
		// on their own.
		{"color", p.env.term.color},
		{"beep", p.env.term.beep},
		// The editor package's, which is a third structure again.
		{"AutoPair", p.env.opts.AutoPair},
	} {
		if !test.got {
			t.Errorf("%s is off by default, so a caller who asked for nothing "+
				"has that feature turned off", test.name)
		}
	}

	// completeAutoTab is the one switch that is off by default, and it is
	// named here so that the list above reads as every switch rather than
	// every switch somebody remembered.
	if p.env.completeAutoTab {
		t.Error("completeAutoTab is on by default, which is not what WithAutoTab implies")
	}
}
