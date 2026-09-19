package capture

import "time"

// Sessions are the recorded sessions. Each one names a golden file in the
// testdata directory of this package.
//
// Every session must end the program, or the recording runs until its limit.
// Typing "exit" ends the isocline demo, and so does Ctrl-D.
var Sessions = []Session{
	{
		Name:  "basic",
		About: "type a line, send it, then leave",
		Steps: []Step{
			{Send: "hello" + KeyEnter},
			{Send: "exit" + KeyEnter},
		},
	},
	{
		Name:  "completion",
		About: "complete a prefix with the Tab key",
		Steps: []Step{
			{Send: "p"},
			{Send: KeyTab},
			{Send: KeyEnter},
			{Send: "exit" + KeyEnter},
		},
	},
	{
		Name:  "completion-menu",
		About: "press Tab twice to open the completion menu",
		Steps: []Step{
			{Send: "f"},
			{Send: KeyTab},
			{Send: KeyTab},
			// The demo cannot tell the Escape key from the start of an
			// escape sequence without waiting, and it waits 200ms on macOS
			// against 100ms on Linux. The quiet period has to be longer than
			// that or the wait ends in the middle of what the demo is
			// writing, and the output lands in two exchanges on one run and
			// one on the next. This waits well past both.
			{Send: KeyEscape, Wait: 600 * time.Millisecond},
			{Send: CtrlU},
			{Send: "exit" + KeyEnter},
		},
	},
	{
		Name:  "highlight",
		About: "type a word that the highlighter colors",
		Steps: []Step{
			{Send: "fun"},
			{Send: " int"},
			{Send: KeyEnter},
			{Send: "exit" + KeyEnter},
		},
	},
	{
		Name:  "editing",
		About: "move the cursor and delete text",
		Steps: []Step{
			{Send: "one two three"},
			{Send: CtrlA},
			{Send: KeyRight + KeyRight + KeyRight},
			{Send: KeyBackspace},
			{Send: CtrlE},
			{Send: CtrlW},
			{Send: CtrlK},
			{Send: KeyEnter},
			{Send: "exit" + KeyEnter},
		},
	},
	{
		Name:  "multiline",
		About: "start a second line with Shift-Tab, then send both",
		Steps: []Step{
			{Send: "first"},
			{Send: KeyShiftTab},
			{Send: "second"},
			{Send: KeyEnter},
			{Send: "exit" + KeyEnter},
		},
	},
	{
		Name:  "history",
		About: "walk back through the history with the Up key",
		Steps: []Step{
			{Send: "alpha" + KeyEnter},
			{Send: "beta" + KeyEnter},
			{Send: KeyUp},
			{Send: KeyUp},
			{Send: KeyDown},
			{Send: KeyEnter},
			{Send: "exit" + KeyEnter},
		},
	},
	{
		Name:  "history-search",
		About: "search the history with Ctrl-R",
		Steps: []Step{
			{Send: "alpha" + KeyEnter},
			{Send: "beta" + KeyEnter},
			{Send: CtrlR},
			{Send: "al"},
			{Send: KeyEnter},
			{Send: KeyEnter},
			{Send: "exit" + KeyEnter},
		},
	},
	{
		Name:  "help",
		About: "open the help screen with F1",
		Steps: []Step{
			{Send: KeyF1},
			{Send: CtrlU},
			{Send: "exit" + KeyEnter},
		},
	},
	{
		Name:  "utf8",
		About: "type text outside ASCII, including a wide character",
		Steps: []Step{
			{Send: "αβγ"},
			{Send: " 日本語"},
			{Send: KeyBackspace},
			{Send: KeyEnter},
			{Send: "exit" + KeyEnter},
		},
	},
	{
		Name:  "hangul-jamo",
		About: "type Hangul Jamo, where the C width table and go-runewidth disagree",
		// U+1100 to U+115F are width 2 in both tables. U+1160 to U+11FF are
		// width 0 in the C table and width 1 in go-runewidth, and isocline
		// computes cursor movement from that width. This session types one
		// decomposed syllable, so the disagreement shows up as a difference in
		// the recorded cursor positions once the Go port replays it. Without
		// this session no recording touches a divergent code point at all.
		Steps: []Step{
			{Send: "\u1100\u1161\u11a8"},
			{Send: KeyEnter},
			{Send: "exit" + KeyEnter},
		},
	},
	{
		Name:  "ctrl-d",
		About: "leave with Ctrl-D instead of typing exit",
		Steps: []Step{
			{Send: "kept" + KeyEnter},
			{Send: CtrlD},
		},
	},
	{
		Name:  "narrow",
		About: "wrap a long line in a narrow terminal",
		Cols:  40,
		Rows:  10,
		Steps: []Step{
			{Send: "0123456789012345678901234567890123456789012345"},
			{Send: KeyEnter},
			{Send: "exit" + KeyEnter},
		},
	},
}
