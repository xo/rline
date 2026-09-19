package capture

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
			{Send: KeyEscape},
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
