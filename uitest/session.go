package main

import "time"

// A Session is a scripted run: what to type, in order, and how long to let
// the program answer before typing the next thing.
//
// It is deliberately the same shape as internal/capture's Session, because
// the two are the same idea at different layers. That one sends bytes to a
// pseudo-terminal; this one types keys at a terminal emulator and lets the
// emulator decide which bytes those become.
type Session struct {
	// Name identifies the session and names its artifacts.
	Name string

	// About says what the session exercises, for whoever reads the
	// screenshot later and wonders what they are looking at.
	About string

	// Keys are typed in order. See Step.
	Keys []Step

	// Quiet is how long the program must be left alone after the last key
	// before the screen is photographed. Zero means DefaultQuiet.
	Quiet time.Duration

	// Watch says this session produces pictures to look at and no golden
	// log to compare.
	//
	// For a session whose behaviour this harness cannot pin down. The
	// completion menu is the one: whether it is still listening when the
	// next key arrives varies between runs, so its log differs about one run
	// in three. A golden that is usually right is worse than none, because
	// the failures teach whoever sees them to rerun rather than to look.
	//
	// The screenshots are still taken and are still the point of the
	// session. What is given up is the mechanical check, and that is said
	// here rather than left for somebody to infer from a test that flakes.
	Watch bool
}

// A Step is one thing to type.
//
// Text is typed literally. Key names a key that has no character: it is the
// name the platform's injector uses, with modifiers joined by a plus, such as
// "Return", "Up", "ctrl+a" or "alt+BackSpace". Exactly one of the two is set.
//
// The distinction matters more here than it looks. Typing the letter a and
// pressing ctrl+a reach the program as different bytes, and which bytes they
// are is the emulator's decision rather than ours. That decision is most of
// what these tests exist to record.
type Step struct {
	Text string
	Key  string

	// Cols and Rows, when both are set, resize the window instead of typing.
	//
	// A resize is the one thing a person does to a terminal that the program
	// has to answer without being asked: the width changes underneath it, a
	// line that fitted no longer does, and the editor has to work out where
	// the cursor now is and repaint from there. It is also where the corpus
	// can say least, because the recording was made at one size.
	Cols, Rows int

	// Wait overrides the session's quiet period after this step.
	Wait time.Duration
}

// Typing returns a Step that types text.
func Typing(text string) Step { return Step{Text: text} }

// Press returns a Step that presses a named key.
func Press(key string) Step { return Step{Key: key} }

// Resize returns a Step that resizes the window to cols by rows.
func Resize(cols, rows int) Step { return Step{Cols: cols, Rows: rows} }

// Default limits.
const (
	// DefaultQuiet is how long to leave the program alone before deciding it
	// has finished drawing. It is longer than internal/capture's 200ms
	// because a terminal emulator has a frame to draw as well, and a
	// screenshot taken mid-frame is a flake nobody will reproduce.
	DefaultQuiet = 400 * time.Millisecond

	// DefaultStart is how long to wait for the terminal to open and the
	// program inside it to draw its first prompt. Opening a window, creating
	// a GPU surface and loading fonts is slower than anything that happens
	// afterwards.
	DefaultStart = 5 * time.Second

	// Cols and Rows are the terminal size every session runs at. Fixed, so
	// that a screenshot from one run can be held against another.
	Cols = 80
	Rows = 24

	// hintSettle is the wait after a step in a session that is about hints
	// or completion.
	//
	// Those are the one thing here that quiescence cannot time. The hint is
	// drawn after rline's own delay of no typing — DefaultHintDelay, 400ms —
	// so the log goes quiet *before* the hint appears, and a step that waits
	// only for quiet sends its next key into the gap. That is not
	// theoretical: a completion session flaked once in three runs, Tab
	// arriving while the hint was still pending and completing nothing.
	//
	// So these steps wait a fixed time that comfortably clears the delay
	// rather than waiting for the log. Anything here that changes
	// DefaultHintDelay has to change this with it.
	hintSettle = 1500 * time.Millisecond
)

// sessions are the scripted runs. Keep each one small and about one thing:
// the artifact is a picture, and a picture of twelve different features tells
// nobody which of them regressed.
var sessions = []Session{
	{
		// The one that becomes the recording in the top-level README, which
		// is why it is paced rather than terse: it has to be watchable at
		// normal speed by somebody who has never seen the library.
		//
		// Tab offers the rest of a word as dimmed text ahead of the cursor
		// rather than opening a menu, and End takes it. Typing over the
		// offer instead leaves the hint showing, and a Return while one is
		// showing is taken as accepting it rather than as ending the row,
		// which is how the first version of this session ended up on one
		// line with no continuation rows at all.
		Name:  "demo",
		About: "The README recording: completion, highlighting as each word lands, a statement continued over rows, and moving back up into one.",
		Keys: []Step{
			Step{Text: "sel", Wait: 500 * time.Millisecond},
			Step{Key: "Tab", Wait: 900 * time.Millisecond},
			Step{Key: "End", Wait: 600 * time.Millisecond},
			Step{Text: " name, email", Wait: 800 * time.Millisecond},
			Press("Return"),
			Step{Text: "from users", Wait: 800 * time.Millisecond},
			Press("Return"),
			Step{Text: "where active = true", Wait: 900 * time.Millisecond},
			Step{Key: "Up", Wait: 600 * time.Millisecond},
			Step{Key: "Home", Wait: 500 * time.Millisecond},
			Step{Key: "End", Wait: 500 * time.Millisecond},
			Step{Key: "Down", Wait: 500 * time.Millisecond},
			Step{Key: "End", Wait: 400 * time.Millisecond},
			Step{Text: ";", Wait: 800 * time.Millisecond},
			Press("Return"),
		},
		Quiet: 600 * time.Millisecond,
	},
	{
		Name:  "basic",
		About: "A line is typed and submitted, and the prompt returns.",
		Keys: []Step{
			Typing("select 1;"),
			Press("Return"),
		},
	},
	{
		Name:  "arrows",
		About: "Every movement key, one at a time. The cursor column after each is the whole point, so this is the session to watch rather than read.",
		Keys: []Step{
			Typing("select alpha, beta from gamma;"),
			Press("Home"),
			Press("Right"), Press("Right"), Press("Right"),
			Press("End"),
			Press("Left"), Press("Left"), Press("Left"),
			Press("ctrl+Left"),
			Press("ctrl+Left"),
			Press("ctrl+Right"),
			Press("Home"),
			Press("End"),
		},
	},
	{
		Name:  "editing",
		About: "An edit in the middle of a line, which is where a redraw has to repaint the tail rather than append to it.",
		Keys: []Step{
			Typing("select frm x;"),
			Press("Home"),
			Press("Right"), Press("Right"), Press("Right"), Press("Right"),
			Press("Right"), Press("Right"), Press("Right"),
			Typing("o"),
			Press("End"),
			Press("BackSpace"),
			Typing(" where y = 1;"),
		},
	},
	{
		Name:  "multiline",
		About: "A statement over several rows, which is what usql does. The continuation prompt and the repaint of the rows above it are what to look at.",
		Keys: []Step{
			Typing("select"),
			Press("Return"),
			Typing("  alpha,"),
			Press("Return"),
			Typing("  beta"),
			Press("Return"),
			Typing("from gamma;"),
			Press("Return"),
		},
	},
	{
		Name:  "multiline-edit",
		About: "Going back up into an earlier row and changing it. The editor has to repaint rows it had finished with, and put the cursor back on a row that is not the last one.",
		Keys: []Step{
			Typing("select"),
			Press("Return"),
			Typing("  alpha,"),
			Press("Return"),
			Typing("  beta"),
			Press("Up"),
			Press("End"),
			Press("BackSpace"),
			Typing("A,"),
			Press("Up"),
			Press("Home"),
			Press("Down"),
			Press("Down"),
			Press("End"),
		},
	},
	{
		Name:  "history",
		About: "Walking back into earlier lines and returning. Each step replaces the whole line, so a redraw that leaves any of the old one behind shows here.",
		Keys: []Step{
			Typing("select 1;"), Press("Return"),
			Typing("select 22222;"), Press("Return"),
			Typing("select 3;"), Press("Return"),
			Press("Up"), Press("Up"), Press("Up"),
			Press("Down"),
			Press("Down"),
		},
	},
	{
		Name:  "wrap",
		About: "A line longer than the window, to see where the terminal wraps and whether the port agrees with it about the last column.",
		Keys: []Step{
			Typing("select 'aaaaaaaaaabbbbbbbbbbccccccccccddddddddddeeeeeeeeeeffffffffffgggggggggghhhhhhhhhh';"),
			Press("Home"),
			Press("End"),
		},
	},
	{
		Name:  "resize-narrow",
		About: "A line that fits, then a window too narrow for it. The text has to rewrap and the cursor has to land where the text now is rather than where it was.",
		Keys: []Step{
			Typing("select alpha, beta, gamma, delta from a_table_with_a_long_name;"),
			Resize(40, 24),
			Resize(100, 24),
			Press("Home"),
			Press("End"),
		},
	},
	{
		Name:  "resize-multiline",
		About: "The same, with a statement already spread over rows. The editor has to work out how many rows it used to occupy and how many it does now.",
		Keys: []Step{
			Typing("select"), Press("Return"),
			Typing("  alpha, beta, gamma, delta, epsilon,"), Press("Return"),
			Typing("  zeta"),
			Resize(45, 24),
			Resize(80, 18),
			Press("Up"),
			Press("End"),
		},
	},
	{
		Name:  "modified-keys",
		About: "The keys that have no character, which every emulator encodes differently. The byte log matters more than the picture here.",
		Keys: []Step{
			Typing("one two three"),
			Press("ctrl+a"),
			Press("ctrl+e"),
			Press("alt+b"),
			Press("ctrl+Left"),
			Press("ctrl+Right"),
			Press("ctrl+w"),
			Press("ctrl+u"),
		},
	},
	{
		Name:  "completion",
		Watch: true,
		About: "Tab completion: a menu for an ambiguous prefix, picking from it, a unique prefix that fills itself in, and a prefix that matches nothing.",
		Keys: []Step{
			// Three words begin with this, so Tab draws a numbered menu
			// under the line rather than completing anything. The menu is
			// extra rows the editor has to take back when it closes, which
			// is the part worth watching.
			Step{Text: "se", Wait: hintSettle},
			Step{Key: "Tab", Wait: hintSettle},
			// Escape closes it again, which is the part that has to put the
			// rows back.
			//
			// Picking an entry is deliberately not scripted here, and that
			// is a limitation rather than a decision about what matters.
			// Typing the number picked the entry on some runs and was
			// inserted as a plain character on others — "se2" instead of
			// "select" — so whether the menu is still listening when the
			// next key arrives is not something this harness can currently
			// pin down. A golden that is right two times in three is worse
			// than no golden, so the menu is opened and closed here, and
			// picking from it is an open question in PLAN.md.
			Step{Key: "Escape", Wait: hintSettle},
			// Nothing begins with this, so Tab has nothing to offer and the
			// line has to be left exactly as it was.
			Step{Text: " zzz", Wait: hintSettle},
			Step{Key: "Tab", Wait: hintSettle},
		},
	},
	{
		Name:  "hints",
		About: "The hint that appears on its own after a pause, without Tab. It is drawn after DefaultHintDelay of no typing, so this session waits longer than that and then types through it.",
		Keys: []Step{
			// Longer than DefaultHintDelay, which is 400ms, so the hint has
			// time to be drawn. A shorter wait here would pass while testing
			// nothing, which is the failure this whole repository keeps
			// finding, so the wait is deliberate rather than inherited.
			Step{Text: "whe", Wait: hintSettle},
			// Typing on leaves the hint behind and redraws from the cursor:
			// what is on screen has to be the typed text and not the hint
			// that was covering that column a moment ago.
			Step{Text: "n", Wait: hintSettle},
			// Backspacing back to the prefix brings the hint again.
			Step{Key: "BackSpace", Wait: hintSettle},
			// And a space ends the word, so there is nothing left to hint.
			Step{Text: " ", Wait: hintSettle},
		},
	},
	{
		Name:  "wide-characters",
		About: "Characters wider than one cell and a grapheme cluster, where the terminal's idea of width and the port's have to agree or the cursor lands in the wrong column.",
		Keys: []Step{
			Typing("-- 日本語 and 👨‍👩‍👧 end"),
			Press("Home"),
			Press("End"),
			Press("BackSpace"),
		},
	},
}
