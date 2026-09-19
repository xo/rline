//go:build !darwin

package rline

// wrapMark is drawn at the end of a row that filled the terminal rather than
// being ended by the user. See wrapmark_darwin.go for why this is a compile
// time choice rather than a runtime one.
const wrapMark = "[ic-dim]←"
