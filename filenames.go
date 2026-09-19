package rline

import (
	"fmt"
	"io/fs"
	"os"
	"strings"
)

// Completing a file name, and colouring what the menu shows for it.
//
// This is the first part of the port that reads the world rather than a
// string, so a corpus of recorded calls cannot be the whole test. The probe
// builds a fixed tree in a temporary directory and completes against it, and
// the Go test builds the same tree and does the same.
//
// Ported from isocline/src/completers.c.

// dirSeparator is what separates the parts of a path.
//
// Windows uses a backslash, and this will need a file per system when
// Windows is taken on. That is deferred until there is a Windows host to
// check it on, and PLAN.md says so.
const dirSeparator = '/'

// fileType says what kind of entry a name is, which decides its colour. The
// order is the one the BSD setting uses, because that setting is a string of
// two letters per type in exactly this order.
type fileType int

const (
	ftDefault fileType = iota
	ftDir
	ftSym
	ftSock
	ftPipe
	ftBlock
	ftChar
	ftSetuid
	ftSetgid
	ftDirOtherWritableSticky
	ftDirOtherWritable
	ftDirSticky
	ftExe
	ftLast
)

// lsColorNames are the keys the GNU setting uses for each file type, in the
// same order as the types.
var lsColorNames = []string{
	"no=", "di=", "ln=", "so=", "pi=", "bd=", "cd=", "su=", "sg=", "tw=",
	"ow=", "st=", "ex=",
}

// defaultBSDColors is the BSD setting used when the environment names none.
const defaultBSDColors = "exfxcxdxbxegedabagacad"

// lsColorSetting is how the environment says to colour a listing.
type lsColorSetting struct {
	// enabled is false when colouring is turned off, and then nothing else
	// matters.
	enabled bool

	// gnu is the GNU style setting, and hasGNU says it was given. An empty
	// one that was given still wins over the BSD style, and then nothing
	// matches.
	gnu    string
	hasGNU bool

	// bsd is the BSD style setting, which is used when the GNU one is absent.
	bsd string
}

// readLSColorSetting reads the colour settings from the environment.
//
// The C code reads these once and keeps them, so a program that changes them
// later keeps the first answer. This reads them each time, which is the same
// for any program that sets them before it starts and makes the behaviour
// testable.
func readLSColorSetting() lsColorSetting {
	s, ok := os.LookupEnv("CLICOLOR")
	if !ok || (s != "1" && s != "") {
		return lsColorSetting{}
	}
	setting := lsColorSetting{enabled: true, bsd: defaultBSDColors}
	if v, ok := os.LookupEnv("LS_COLORS"); ok {
		setting.gnu, setting.hasGNU = v, true
	}
	if v, ok := os.LookupEnv("LSCOLORS"); ok {
		setting.bsd = v
	}
	return setting
}

// colorFromKey appends the markup for one key of the GNU setting, and reports
// whether the key was there.
//
// The key is looked for anywhere in the setting rather than only at the start
// of an entry, so a key can match inside a longer one. The C code does the
// same.
func colorFromKey(b *strings.Builder, setting, key string) bool {
	if key == "" {
		return false
	}
	i := strings.Index(setting, key)
	if i < 0 {
		return false
	}
	rest := setting[i+len(key):]
	if key[len(key)-1] != '=' {
		// A file type key already ends with the equals sign. An extension
		// key does not, so one has to follow it.
		if rest == "" || rest[0] != '=' {
			return false
		}
		rest = rest[1:]
	}
	end := strings.IndexByte(rest, ':')
	if end < 0 {
		end = len(rest)
	}
	if end <= 0 {
		return false
	}
	b.WriteString(`[ansi-sgr="`)
	b.WriteString(rest[:end])
	b.WriteString(`"]`)
	return true
}

// colorFromChar reads one letter of the BSD setting as a colour number. The
// lower case letters are the first eight colours, the upper case letters the
// next eight, and anything else means the default.
func colorFromChar(c byte) int {
	switch {
	case c >= 'a' && c <= 'h':
		return int(c - 'a')
	case c >= 'A' && c <= 'H':
		return int(c-'A') + 8
	}
	return 256
}

// appendLSColor appends the markup that opens the colour for a file type, and
// reports whether it opened one. ext is the extension to look for, which only
// the GNU setting uses and only for an ordinary file.
func appendLSColor(b *strings.Builder, ft fileType, ext string, hasExt bool) bool {
	setting := readLSColorSetting()
	if !setting.enabled {
		return false
	}
	if setting.hasGNU {
		if ft == ftDefault && hasExt {
			if colorFromKey(b, setting.gnu, ext) {
				return true
			}
		}
		if ft >= ftDefault && ft < ftLast {
			if colorFromKey(b, setting.gnu, lsColorNames[ft]) {
				return true
			}
		}
		return false
	}
	// The BSD setting is two letters per file type, a foreground and a
	// background. A setting too short for this type leaves both at default.
	fg, bg := byte('x'), byte('x')
	if len(setting.bsd) > 2*int(ft)+1 {
		fg, bg = setting.bsd[2*ft], setting.bsd[2*ft+1]
	}
	fmt.Fprintf(b, "[ansi-color=%d ansi-bgcolor=%d]", colorFromChar(fg), colorFromChar(bg))
	return true
}

// colorizeEntry returns the markup the menu shows for one entry: the colour
// for its type, then the name marked so that the markup parser does not read
// the name itself as markup.
func colorizeEntry(noColor bool, ft fileType, name, ext string, hasExt bool, dirSep byte) string {
	var b strings.Builder
	opened := false
	if !noColor {
		opened = appendLSColor(&b, ft, ext, hasExt)
	}
	b.WriteString("[!pre]")
	b.WriteString(name)
	if dirSep != 0 {
		b.WriteByte(dirSep)
	}
	b.WriteString("[/pre]")
	if opened {
		b.WriteString("[/]")
	}
	return b.String()
}

// endsWith reports whether name ends with ending. An empty ending matches
// anything.
func endsWith(name, ending string) bool {
	if len(name) < len(ending) {
		return false
	}
	if ending == "" {
		return true
	}
	return name[len(name)-len(ending):] == ending
}

// matchExtension reports whether name ends with one of the extensions, which
// are separated by semicolons.
//
// An empty list matches everything. So does a list with an empty part in it,
// because an empty extension matches any name: ".txt;" matches everything,
// not only names ending in .txt.
func matchExtension(name, extensions string) bool {
	if extensions == "" {
		return true
	}
	for _, ext := range strings.Split(extensions, ";") {
		if endsWith(name, ext) {
			return true
		}
	}
	return false
}

// typeOf returns the file type of path, for colouring.
//
// It does not follow a symbolic link, so a link is a link whatever it points
// at. A path it cannot read is an ordinary file, which is what the C code
// ends up with when its stat fails and it reads the zeroed structure.
func typeOf(path string) fileType {
	info, err := os.Lstat(path)
	if err != nil {
		return ftDefault
	}
	mode := info.Mode()
	switch {
	case mode&fs.ModeSocket != 0:
		return ftSock
	case mode&fs.ModeSymlink != 0:
		return ftSym
	case mode&fs.ModeNamedPipe != 0:
		return ftPipe
	case mode&fs.ModeCharDevice != 0:
		return ftChar
	case mode&fs.ModeDevice != 0:
		return ftBlock
	case mode&fs.ModeDir != 0:
		// Only a directory is looked at for these. A file that is set-user
		// or sticky is still just a file, which is what the C code does
		// because it tests them inside its directory branch.
		switch {
		case mode&fs.ModeSetuid != 0:
			return ftSetuid
		case mode&fs.ModeSetgid != 0:
			return ftSetgid
		case mode.Perm()&0o020 != 0 && mode&fs.ModeSticky != 0:
			return ftDirOtherWritableSticky
		case mode.Perm()&0o020 != 0:
			return ftDirOtherWritable
		case mode&fs.ModeSticky != 0:
			return ftDirSticky
		}
		return ftDir
	}
	if mode.Perm()&0o100 != 0 {
		return ftExe
	}
	return ftDefault
}

// isDir reports whether path is a directory, following a symbolic link, so
// that a link to a directory completes with a separator after it.
func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// isAbsolutePath reports whether path starts at the root.
func isAbsolutePath(path string) bool {
	return path != "" && path[0] == dirSeparator
}

// completeInDir offers every entry of dir that starts with base.
//
// dirPrefix is what goes in front of each name in the completion, which is
// the part of the path the user already typed. It reports false once no more
// completions are accepted.
func completeInDir(cenv *completionEnv, noColor bool, dir, dirPrefix, base string,
	dirSep byte, extensions string,
) bool {
	f, err := os.Open(dir)
	if err != nil {
		return true
	}
	defer func() { _ = f.Close() }()
	// Read in directory order rather than sorted, which is what the C sees.
	// Nothing depends on the order, because the menu sorts before it shows.
	entries, err := f.ReadDir(-1)
	if err != nil {
		return true
	}
	for _, entry := range entries {
		name := entry.Name()
		if name == "." || name == ".." || !hasPrefixFold(name, base) {
			continue
		}
		full := dir + string(dirSeparator) + name
		ft := typeOf(full)
		directory := isDir(full)

		replacement := dirPrefix + name
		if directory && dirSep != 0 {
			replacement += string(dirSep)
		}
		if !directory && !matchExtension(name, extensions) {
			continue
		}
		shown := byte(0)
		if directory {
			shown = dirSep
		}
		// The file completer never passes an extension to look up, so the
		// extension colours of the GNU setting are never reached from here.
		// The C code is the same.
		display := colorizeEntry(noColor, ft, name, "", false, shown)
		if !cenv.add(replacement, display, "", 0, 0) {
			return false
		}
	}
	return true
}

// filenameCompleter offers the file names that could follow prefix.
func filenameCompleter(cenv *completionEnv, prefix string, noColor bool,
	dirSep byte, roots, extensions string,
) {
	// Split what was typed into the directory part and the start of a name.
	dirPrefix, base := "", prefix
	if i := strings.LastIndexByte(prefix, dirSeparator); i >= 0 {
		dirPrefix, base = prefix[:i+1], prefix[i+1:]
	}

	if isAbsolutePath(prefix) {
		// An absolute path is completed where it points rather than under
		// any of the roots.
		completeInDir(cenv, noColor, dirPrefix, dirPrefix, base, dirSep, extensions)
		return
	}
	// A relative path is completed under each root in turn. The C code does
	// not stop when no more completions are accepted, and neither does this.
	for _, root := range strings.Split(roots, ";") {
		dir := root + string(dirSeparator)
		if dirPrefix != "" {
			// Without its trailing separator, because one was just added.
			dir += dirPrefix[:len(dirPrefix)-1]
		}
		completeInDir(cenv, noColor, dir, dirPrefix, base, dirSep, extensions)
	}
}

// completeFilename offers file names for the word at the end of prefix.
//
// roots are the directories a relative name is looked for in, separated by
// semicolons, and extensions the endings a name must have, also separated by
// semicolons. An empty roots means the working directory, and an empty
// extensions means any name. A dirSep of zero means the one this system uses.
//
// The word is taken with completeQWordEx, so a name with a space in it can be
// completed whether the user quoted it or escaped the space.
func completeFilename(cenv *completionEnv, prefix string, noColor bool,
	dirSep byte, roots, extensions string,
) {
	if roots == "" {
		roots = "."
	}
	if dirSep == 0 {
		dirSep = dirSeparator
	}
	// The C code hands the settings to the completer through the argument
	// that belongs to the program, and leaves it pointing at a dead stack
	// value afterwards. A closure carries them here instead, so the
	// program's own argument is left alone.
	inner := func(cenv *completionEnv, word string) {
		filenameCompleter(cenv, word, noColor, dirSep, roots, extensions)
	}
	completeQWordEx(cenv, prefix, inner, charIsFileNameLetter, defaultEscapeChar, defaultQuoteChars)
}
