package rline

import "testing"

// collect runs a completer over a line and returns what it offered.
func collect(t *testing.T, c *completions, input string, cursor, maxOffers int) []completion {
	t.Helper()
	c.generate(input, cursor, maxOffers)
	return c.items
}

// TestCompleteQWordUsesTheUsualQuoting checks that the short form is the long
// form with a backslash and the two usual quotes.
func TestCompleteQWordUsesTheUsualQuoting(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		input string
	}{
		{"a plain word", "ls fi"},
		{"inside single quotes", "ls 'my fi"},
		{"inside double quotes", "ls \"my fi"},
		{"an escaped space", "ls my\\ fi"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			inner := func(cenv *completionEnv, _ string) {
				cenv.add("my file.txt", "", "", 0, 0)
			}
			short := &completions{}
			short.setCompleter(func(cenv *completionEnv, prefix string) {
				completeQWord(cenv, prefix, inner, nil)
			}, nil)
			long := &completions{}
			long.setCompleter(func(cenv *completionEnv, prefix string) {
				completeQWordEx(cenv, prefix, inner, nil, defaultEscapeChar, defaultQuoteChars)
			}, nil)

			cursor := len(test.input)
			got := collect(t, short, test.input, cursor, 10)
			want := collect(t, long, test.input, cursor, 10)
			if len(got) != len(want) {
				t.Fatalf("the short form offered %d, the long form %d", len(got), len(want))
			}
			for i := range got {
				if got[i] != want[i] {
					t.Errorf("completion %d differs: %+v against %+v", i, got[i], want[i])
				}
			}
		})
	}
}

// TestAddCompletionsFiltersByPrefix checks the helper that offers the members
// of a fixed list that start with what was typed. The comparison ignores
// case, as the C one does.
func TestAddCompletionsFiltersByPrefix(t *testing.T) {
	t.Parallel()
	words := []string{"print", "printf", "println", "parse", "Print3"}
	for _, test := range []struct {
		name   string
		prefix string
		want   []string
	}{
		// Print3 matches too, because the comparison folds case.
		{"a shared start", "pri", []string{"print", "printf", "println", "Print3"}},
		{"case does not matter", "PRI", []string{"print", "printf", "println", "Print3"}},
		{"a start that ignores case", "print3", []string{"Print3"}},
		{"a longer start", "printf", []string{"printf"}},
		{"everything", "", words},
		{"nothing matches", "zz", nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := &completions{}
			c.setCompleter(func(cenv *completionEnv, prefix string) {
				addCompletions(cenv, prefix, words)
			}, nil)
			c.generate(test.prefix, len(test.prefix), 100)
			if c.count() != len(test.want) {
				t.Fatalf("offered %d %v, want %d %q", c.count(), c.items, len(test.want), test.want)
			}
			for i, w := range test.want {
				if got := c.items[i].replacement; got != w {
					t.Errorf("completion %d is %q, want %q", i, got, w)
				}
			}
		})
	}
}

// TestAddCompletionsStopsWhenFull checks that the helper gives up once no
// more are accepted, rather than walking the whole list.
func TestAddCompletionsStopsWhenFull(t *testing.T) {
	t.Parallel()
	words := []string{"a1", "a2", "a3", "a4", "a5"}
	c := &completions{}
	finished := false
	c.setCompleter(func(cenv *completionEnv, prefix string) {
		finished = addCompletions(cenv, prefix, words)
	}, nil)
	c.generate("a", 1, 2)
	if finished {
		t.Error("the helper reported it got through the whole list")
	}
	if got := c.count(); got != 2 {
		t.Errorf("it offered %d, want 2", got)
	}
	if !c.stopCompleting() {
		t.Error("stopCompleting said no after the list filled up")
	}
}

// TestHasCompletionsAndStopCompleting checks the two questions a completer
// asks while it works: whether anything has been found yet, and whether to
// give up.
func TestHasCompletionsAndStopCompleting(t *testing.T) {
	t.Parallel()
	c := &completions{}
	if c.hasCompletions() {
		t.Error("an empty list said it has completions")
	}
	if !c.stopCompleting() {
		t.Error("a list that accepts nothing said to keep going")
	}
	c.completerMax = 2
	if c.stopCompleting() {
		t.Error("a list with room said to stop")
	}
	c.add("one", "", "", 0, 0)
	if !c.hasCompletions() {
		t.Error("a list with an entry said it has none")
	}
	c.add("two", "", "", 0, 0)
	if !c.stopCompleting() {
		t.Error("a full list said to keep going")
	}
}

// TestCompletionLimitsAreConsistent checks the two limits against each other.
// The menu shows at most one, and a completer is asked to stop at the other,
// which has to be the smaller of the two or the menu could never fill.
func TestCompletionLimitsAreConsistent(t *testing.T) {
	t.Parallel()
	if maxCompletionsToTry > maxCompletionsToShow {
		t.Errorf("the limit on trying, %d, is above the limit on showing, %d",
			maxCompletionsToTry, maxCompletionsToShow)
	}
	if maxCompletionsToTry <= 0 || maxCompletionsToShow <= 0 {
		t.Errorf("the limits are %d and %d, and both have to leave room for something",
			maxCompletionsToTry, maxCompletionsToShow)
	}
}

// TestGenerateRefusesACursorPastTheEnd checks that a cursor outside the line
// offers nothing rather than reading past it.
func TestGenerateRefusesACursorPastTheEnd(t *testing.T) {
	t.Parallel()
	called := false
	c := &completions{}
	c.setCompleter(func(cenv *completionEnv, prefix string) {
		called = true
		cenv.add("x", "", "", 0, 0)
	}, nil)
	for _, cursor := range []int{-1, 4, 100} {
		called = false
		if got := c.generate("abc", cursor, 10); got != 0 {
			t.Errorf("a cursor at %d offered %d completions, want 0", cursor, got)
		}
		if called {
			t.Errorf("a cursor at %d reached the completer", cursor)
		}
	}
	if got := c.generate("abc", 3, 10); got != 1 {
		t.Errorf("a cursor at the end offered %d, want 1", got)
	}
}

// TestGeneratePassesTheCompleterArg checks that whatever the program handed
// over when it set its completer reaches the completer again.
func TestGeneratePassesTheCompleterArg(t *testing.T) {
	t.Parallel()
	type marker struct{ n int }
	want := &marker{n: 42}
	var got any
	c := &completions{}
	c.setCompleter(func(cenv *completionEnv, _ string) {
		got = cenv.arg
	}, want)
	c.generate("x", 1, 10)
	if got != any(want) {
		t.Errorf("the completer was given %v, want %v", got, want)
	}
}
