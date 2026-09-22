# uitest

Tests that open a real terminal emulator, type into it like a person, and
record both what was sent and what was drawn.

These do not run in CI. They need a desktop, they are slow, and several of
them need software that is not on every machine. They are run by a person,
with one command, and they leave artifacts a person can look at.

    go run ./uitest -list
    go run ./uitest -terminal foot -session basic
    go run ./uitest                       # every terminal found on this machine
    go run ./uitest -update               # bless the current output

    go run ./uitest -video                # record the run to out/session.mp4
    go run ./uitest -vnc                  # watch it live on 127.0.0.1:5900
    go run ./uitest -visible              # run on your own desktop instead

## Where the keys go, which is the important part

On Linux the tests run on a **private compositor**: a headless sway with no
graphics card, its own runtime directory and a fixed 1920x1080 output, started
when the run begins and thrown away when it ends.

That is a safety property rather than a tidiness one. A synthesised keystroke
goes to a compositor, not to a window. `wtype` types wherever the keyboard
focus is at the instant it runs, and checking focus first does not help,
because the check and the keystroke are not the same moment. Early versions of
this harness did check, and twice typed test input into Ken's own windows
anyway — once into a chat message, mid-sentence — because a terminal window
closed between the check and the type and focus moved on.

A private compositor removes the question instead of guarding it. Nothing
reaches the desktop, because the desktop is a different compositor.

It also made the runs reproducible, which they had not been. On a tiling
desktop a terminal opens at the size it asked for and is then resized to fit
the layout; measured here, foot asked for 80x24, got it, and was tiled to
47x174 a second later. Every resize is a redraw, so three runs of one session
produced 86, 66 and 42 lines of log. On the private compositor they are equal.

`-visible` runs on the real desktop instead, and says so loudly. It exists
because watching a run is sometimes what you want, and because macOS and
Windows have no equivalent isolation yet — there the harness shares the
desktop and falls back to the focus check, which is weaker, and the person
running it should leave the machine alone while it does.

## Watching a run

The sessions are a few seconds each and nothing is displayed, so there are
three ways to see what happened.

**Per-step screenshots**, always written, in `out/<terminal>/<session>/`. One
picture after every keystroke, numbered and named, so `03-ctrl-a.png` is what
the screen looked like after ctrl+a. This is usually what you want: a cursor
that lands in the wrong column for one step and is corrected by the next
leaves no trace in the final frame.

**A video**, with `-video`, needing `wf-recorder`. The whole run as one file,
which is where flicker and a redraw that repaints in two stages show up.

**Live**, with `-vnc`, needing `wayvnc`. The harness waits ten seconds after
starting it so there is time to connect a VNC client to 127.0.0.1:5900.

## Why this exists

`internal/capture` drives a pseudo-terminal and compares bytes. A
pseudo-terminal is not a terminal emulator: nothing renders the escape
sequences. The corpus records that the port emitted `\e[2K\e[1G` and that the
bytes match, and it cannot say what a person would have seen — where the
cursor ended up, whether a line wrapped, what the prompt looked like after a
repaint, whether a redraw left something behind.

It also cannot test the other direction. The corpus sends bytes that stand in
for keys. A real terminal turns a keystroke into those bytes itself, and which
bytes it chooses is the terminal's decision, not ours. Shift-Enter, Ctrl-Left
and Alt-Backspace do not exist in ASCII, and every emulator encodes them
differently. Typing into a real one is the only way to find out what the port
actually receives.

## Shape

Three things are recorded per run.

**The byte log**, which is uniform across every terminal and needs nothing
from the emulator. `rline.WithLogger` already records every byte read from
the keyboard, every byte written to the terminal, and each finished line, so
the program under test records itself. This is the part that can be compared
mechanically, and it is where a key-encoding difference shows up.

**A screenshot**, which is the only thing that can show what was drawn. Per
platform, never compared across machines.

**A text grid**, where the terminal can be asked for one. `wezterm cli
get-text --escapes` and `kitty @ get-text` return the screen as cells. Most
terminals cannot do this, so it is a bonus rather than the mechanism.

The keys go in at the OS level rather than into the pseudo-terminal, because
the emulator's own encoding is the thing being tested. Writing to the pty
would bypass exactly what we came to measure.

## The matrix

Distinct engines rather than brands. Several of the popular terminals share
an engine and testing both adds nothing.

| Platform | Terminals |
| --- | --- |
| Linux, X11 and Wayland | wezterm, foot, ghostty, kitty, alacritty, gnome-terminal (VTE), xterm |
| macOS | wezterm, ghostty, iTerm2, Terminal.app |
| Windows | conhost, Windows Terminal, wezterm |

`tmux` belongs in the matrix as a layer rather than a terminal: it sits
between the emulator and the program, rewrites `TERM`, and intercepts keys.

### The Windows row is about hosts, not shells

`cmd.exe`, `pwsh` and `powershell` are shells. The terminal is `conhost.exe`
or Windows Terminal, depending on how the shell was launched. A readline
library owns raw mode and reads the console handle itself, so the shell that
started it does not change key encoding or rendering. What changes is the
host, and conhost and Windows Terminal differ in ways that matter: VT input,
truecolor, synchronized output.

The shells are worth a small cross-product only because they set the initial
console modes that the library then finds.

## What differs between terminals, and is worth catching

Gathered from Gemini and DeepSeek, both asked independently.

- **Key encoding for modified keys.** The Kitty keyboard protocol in kitty,
  ghostty, foot and wezterm against legacy `modifyOtherKeys` in iTerm2 and
  Terminal.app. Also the Ctrl-H, `0x7f` and `\e[3~` disagreement over what
  backspace and delete send.
- **Deferred wrap at the right margin.** Whether writing to the last column
  wraps at once or waits for the next character. This decides how a prompt
  redraws and what backspacing over a margin does.
- **`DECSCUSR` cursor shape.** Terminal.app largely ignores it and conhost
  has mishandled it. This is an open question in PLAN.md and the matrix is
  how it gets answered.
- **Character width.** Terminal.app uses older `wcwidth` tables and breaks on
  ZWJ emoji and flags; the modern engines do grapheme clustering.
- **Bracketed paste**, and whether control characters inside a paste survive.
- **Focus events**, which some terminals drop when a tab changes.
- **Synchronized output**, mode 2026, supported by the modern four and absent
  from Terminal.app and conhost.

## What is deliberately not built

**No pixel comparison across machines.** Font rendering varies by hinting,
subpixel geometry, GPU and OS. Screenshots are artifacts for a person, and
the mechanical comparison is the byte log. Both models said this unprompted
and they are right.

**No terminal emulator of our own.** Writing a parser that turns escape
sequences into a grid means reimplementing VT100, ECMA-48, grapheme
clustering and East Asian width, and then the tests measure that parser.

**No mouse or window-drag testing.** Resize is exercised by asking the
terminal to resize, not by driving a pointer at a window corner.
