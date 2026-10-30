//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Windows.
//
// The matrix here is hosts first. cmd.exe, pwsh and powershell are shells:
// what draws the screen and encodes the keys is conhost.exe or Windows
// Terminal, and which of those a shell gets depends on how it was launched
// rather than on which shell it is. A readline library owns raw mode and reads
// the console handle itself. What the shell changes is the console modes that
// the library finds when it starts, so each host runs the program from
// cmd.exe and from PowerShell (D60).
//
// Everything is driven through PowerShell, because the pieces needed — bring
// a window to the front, send keys to it, photograph it — are in the .NET
// assemblies that ship with the system, and reaching them from Go directly
// would mean a pile of syscall wrappers for no gain.
//
// NOT YET RUN ON WINDOWS. Written from the documented behaviour of SendKeys,
// SetForegroundWindow and the Graphics.CopyFromScreen family. The part most
// likely to be wrong is the key table in typeKey: SendKeys has its own
// notation and does not cover every key, and where it falls short the answer
// is probably SendInput through x/sys/windows rather than more escaping.

const isWindows = true

// appID is the window title this harness gives its terminals, because a
// window on Windows is found by title rather than by class.
const appID = "rline-uitest"

// platformReady reports whether this machine can run the harness.
func platformReady() error {
	if _, err := exec.LookPath("powershell.exe"); err != nil {
		if _, err := exec.LookPath("pwsh.exe"); err != nil {
			return fmt.Errorf("neither powershell.exe nor pwsh.exe is on the path: %w", err)
		}
	}
	return nil
}

// platformTerminals is the Windows matrix of D60. The hosts are conhost and
// Windows Terminal, and each runs the program from cmd.exe and from
// PowerShell, because the shell sets the console modes that rline then finds.
// The VS Code terminal is in the matrix of D60 and is not here yet (backlog
// 74).
func platformTerminals() []Terminal {
	return []Terminal{
		{
			Name: "conhost-cmd", Bin: "conhost.exe", Engine: "conhost",
			Args: func(c, r int, cmd []string) []string {
				// The legacy host. It is still what a program gets when it is
				// started without Windows Terminal, and it is the one that
				// has historically mishandled DECSCUSR and lacked
				// synchronized output, so it is the interesting half of this
				// row rather than the obsolete half.
				inner := fmt.Sprintf("mode con: cols=%d lines=%d & title %s & %s", c, r, appID, strings.Join(cmd, " "))
				return []string{"conhost.exe", "cmd.exe", "/c", inner}
			},
		},
		{
			Name: "conhost-powershell", Bin: "conhost.exe", Engine: "conhost", Needs: []string{"powershell.exe"},
			Args: func(c, r int, cmd []string) []string {
				inner := fmt.Sprintf("mode con: cols=%d lines=%d; $host.UI.RawUI.WindowTitle = '%s'; %s",
					c, r, appID, psCommand(cmd))
				return []string{"conhost.exe", "powershell.exe", "-NoProfile", "-Command", inner}
			},
		},
		{
			Name: "wt-cmd", Bin: "wt.exe", Engine: "Windows Terminal",
			Args: func(c, r int, cmd []string) []string {
				return append([]string{"wt.exe", "--window", "new",
					"--size", fmt.Sprintf("%d,%d", c, r),
					"--title", appID, "--", "cmd.exe", "/c"}, cmd...)
			},
		},
		{
			Name: "wt-powershell", Bin: "wt.exe", Engine: "Windows Terminal", Needs: []string{"powershell.exe"},
			Args: func(c, r int, cmd []string) []string {
				return []string{"wt.exe", "--window", "new",
					"--size", fmt.Sprintf("%d,%d", c, r),
					"--title", appID, "--", "powershell.exe", "-NoProfile", "-Command", psCommand(cmd)}
			},
		},
		{
			Name: "wezterm", Bin: "wezterm.exe", Engine: "wezterm",
			Args: func(c, r int, cmd []string) []string {
				return append([]string{"wezterm.exe",
					"--config", fmt.Sprintf("initial_cols=%d", c),
					"--config", fmt.Sprintf("initial_rows=%d", r),
					"--config", "cursor_blink_rate=0",
					"start", "--class", appID, "--"}, cmd...)
			},
			GetText: weztermGetText,
		},
	}
}

// psCommand returns a PowerShell command that runs cmd, with each word in
// single quotes so that a path with a space in it stays one word.
func psCommand(cmd []string) string {
	words := make([]string, len(cmd))
	for i, w := range cmd {
		words[i] = "'" + strings.ReplaceAll(w, "'", "''") + "'"
	}
	return "& " + strings.Join(words, " ")
}

// focusWindow brings the terminal to the front.
func focusWindow(t Terminal) error {
	script := `
$sig = @"
[DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr hWnd);
[DllImport("user32.dll")] public static extern bool ShowWindow(IntPtr hWnd, int nCmdShow);
"@
$w = Add-Type -MemberDefinition $sig -Name Win -Namespace Native -PassThru
$p = Get-Process | Where-Object { $_.MainWindowTitle -like "*` + appID + `*" } | Select-Object -First 1
if ($p -eq $null) { Write-Error "no window titled ` + appID + `"; exit 1 }
$w::ShowWindow($p.MainWindowHandle, 9) | Out-Null
$w::SetForegroundWindow($p.MainWindowHandle) | Out-Null
`
	if err := pwsh(script); err != nil {
		return fmt.Errorf("%s opened but its window could not be brought to the front: %w", t.Name, err)
	}
	time.Sleep(500 * time.Millisecond)
	return nil
}

// ensureFocused refuses to type unless the harness's own window is in front.
//
// The same rule as every other platform. Synthetic keys go to whatever has
// focus, so a harness that does not check will type into whatever the person
// is using, and on this platform that is as easy to do as on the others.
func ensureFocused() error {
	out, err := pwshOut(`
$sig = @"
[DllImport("user32.dll")] public static extern IntPtr GetForegroundWindow();
[DllImport("user32.dll")] public static extern int GetWindowText(IntPtr hWnd, System.Text.StringBuilder t, int n);
"@
$w = Add-Type -MemberDefinition $sig -Name Win2 -Namespace Native -PassThru
$sb = New-Object System.Text.StringBuilder 512
$w::GetWindowText($w::GetForegroundWindow(), $sb, 512) | Out-Null
$sb.ToString()
`)
	if err != nil {
		return fmt.Errorf("cannot tell which window is in front, so refusing to type: %w", err)
	}
	title := strings.TrimSpace(out)
	if !strings.Contains(title, appID) {
		return fmt.Errorf("the window in front is %q rather than the harness's own %q, so refusing to type into it", title, appID)
	}
	return nil
}

// typeText types literal text.
func typeText(s string) error {
	if err := ensureFocused(); err != nil {
		return err
	}
	return sendKeys(escapeSendKeys(s))
}

// typeKey presses a named key.
//
// The sessions name keys the X11 way, so they are translated here. SendKeys
// writes modifiers as prefixes: ^ for control, % for alt, + for shift.
func typeKey(name string) error {
	if err := ensureFocused(); err != nil {
		return err
	}
	parts := strings.Split(name, "+")
	key := parts[len(parts)-1]
	prefix := ""
	for _, m := range parts[:len(parts)-1] {
		switch m {
		case "ctrl":
			prefix += "^"
		case "alt":
			prefix += "%"
		case "shift":
			prefix += "+"
		default:
			return fmt.Errorf("unknown modifier %q", m)
		}
	}
	names := map[string]string{
		"Return": "{ENTER}", "Tab": "{TAB}", "Escape": "{ESC}",
		"BackSpace": "{BACKSPACE}", "Delete": "{DELETE}",
		"Left": "{LEFT}", "Right": "{RIGHT}", "Up": "{UP}", "Down": "{DOWN}",
		"Home": "{HOME}", "End": "{END}", "Prior": "{PGUP}", "Next": "{PGDN}",
	}
	if n, ok := names[key]; ok {
		return sendKeys(prefix + n)
	}
	if len([]rune(key)) == 1 {
		return sendKeys(prefix + escapeSendKeys(key))
	}
	return fmt.Errorf("no SendKeys name for %q", key)
}

// escapeSendKeys quotes the characters SendKeys reads as instructions.
func escapeSendKeys(s string) string {
	r := strings.NewReplacer(
		"{", "{{}", "}", "{}}", "+", "{+}", "^", "{^}", "%", "{%}",
		"~", "{~}", "(", "{(}", ")", "{)}", "[", "{[}", "]", "{]}")
	return r.Replace(s)
}

// sendKeys sends one SendKeys string to the focused window.
func sendKeys(s string) error {
	return pwsh(`[void][System.Reflection.Assembly]::LoadWithPartialName("System.Windows.Forms")
[System.Windows.Forms.SendKeys]::SendWait(` + psQuote(s) + `)`)
}

// resizeWindow resizes the terminal to cols by rows.
//
// In pixels, scaled from the window's current size, because only conhost
// takes a size in characters and it takes it through mode con rather than
// through the window.
func resizeWindow(cols, rows int) error {
	return pwsh(fmt.Sprintf(`
$sig = @"
[DllImport("user32.dll")] public static extern IntPtr GetForegroundWindow();
[DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr hWnd, out System.Drawing.Rectangle r);
[DllImport("user32.dll")] public static extern bool MoveWindow(IntPtr hWnd, int x, int y, int w, int h, bool repaint);
"@
[void][System.Reflection.Assembly]::LoadWithPartialName("System.Drawing")
$w = Add-Type -MemberDefinition $sig -Name Win3 -Namespace Native -PassThru -ReferencedAssemblies System.Drawing
$h = $w::GetForegroundWindow()
$r = New-Object System.Drawing.Rectangle
$w::GetWindowRect($h, [ref]$r) | Out-Null
$width  = ($r.Width  - $r.X) * %d / %d
$height = ($r.Height - $r.Y) * %d / %d
$w::MoveWindow($h, $r.X, $r.Y, [int]$width, [int]$height, $true) | Out-Null
`, cols, Cols, rows, Rows))
}

// screenshot photographs the window in front.
func screenshot(_ Terminal, path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolving the screenshot path: %w", err)
	}
	return pwsh(fmt.Sprintf(`
$sig = @"
[DllImport("user32.dll")] public static extern IntPtr GetForegroundWindow();
[DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr hWnd, out System.Drawing.Rectangle r);
"@
[void][System.Reflection.Assembly]::LoadWithPartialName("System.Drawing")
$w = Add-Type -MemberDefinition $sig -Name Win4 -Namespace Native -PassThru -ReferencedAssemblies System.Drawing
$r = New-Object System.Drawing.Rectangle
$w::GetWindowRect($w::GetForegroundWindow(), [ref]$r) | Out-Null
$bmp = New-Object System.Drawing.Bitmap (($r.Width - $r.X), ($r.Height - $r.Y))
$g = [System.Drawing.Graphics]::FromImage($bmp)
$g.CopyFromScreen($r.X, $r.Y, 0, 0, $bmp.Size)
$bmp.Save(%s, [System.Drawing.Imaging.ImageFormat]::Png)
`, psQuote(abs)))
}

// weztermGetText asks wezterm for its screen.
func weztermGetText() (string, error) {
	out, err := exec.Command("wezterm.exe", "cli", "get-text", "--escapes").Output()
	return string(out), err
}

// psQuote returns a PowerShell single-quoted string.
func psQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

// pwsh runs a script and discards its output.
func pwsh(script string) error {
	_, err := pwshOut(script)
	return err
}

// pwshOut runs a script and returns what it printed.
func pwshOut(script string) (string, error) {
	shell := "powershell.exe"
	if _, err := exec.LookPath(shell); err != nil {
		shell = "pwsh.exe"
	}
	cmd := exec.Command(shell, "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%s: %w", shell, err)
	}
	return string(out), nil
}

// setFontSize is accepted and ignored: the terminals here take their font
// from their own settings rather than from a command line.
func setFontSize(int) {}
