//go:build !darwin

package rline

// wrapMark is drawn at the end of a row that filled the terminal rather than
// being ended by the user. See wrapmark_darwin.go for why this is a compile
// time choice rather than a runtime one.
const wrapMark = "[ic-dim]←"

// corpusVariant names the recordings this build compares against. Every system
// but macOS draws the same glyph, so they all share one set.
//
// "default" means two systems agreeing rather than one system's answer. The
// set is recorded on Linux and Windows compares against it, and Windows has
// been shown to match all 1584 redraws. That holds because the port draws the
// same bytes everywhere: it has no console emulation path of the kind term.c
// compiles on Windows, and assumes a console that reads escape sequences. If
// that ever stops being true, Windows needs a variant of its own, and the
// failure will be loud rather than quiet, because a Windows build comparing
// against a Linux recording is what would break.
const corpusVariant = "default"
