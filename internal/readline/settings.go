package readline

import (
	"github.com/xo/inputrc"

	"github.com/xo/rline/key"
)

// settings are the variables of readline that the commands of this package
// read, set from an inputrc file with Configure.
type settings struct {
	// disableCompletion makes the completion commands type their key, as
	// readline's disable-completion does.
	disableCompletion bool

	// showAllIfAmbiguous lists the completions at the first complete when
	// there are several, and showAllIfUnmodified when there are several and
	// nothing could be filled in.
	showAllIfAmbiguous  bool
	showAllIfUnmodified bool

	// queryItems is completion-query-items: how many completions are listed
	// without asking first.
	queryItems int

	// isearchTerminators end an incremental search and are not run.
	isearchTerminators []key.Code

	// commentBegin is what insert-comment puts at the start of the line.
	commentBegin string
}

// defaultSettings are readline's defaults.
func defaultSettings() settings {
	return settings{
		queryItems:         queryItems,
		isearchTerminators: []key.Code{key.Esc, key.Linefeed},
		commentBegin:       "#",
	}
}

// Configure sets the variables of readline that this package reads from vars,
// which holds them by readline's names, as an inputrc file sets them. It
// returns the names that it does not know, so that the program can say that
// they do nothing.
func (l *Line) Configure(vars map[string]any) []string {
	var unknown []string
	for name, v := range vars {
		switch name {
		case "disable-completion":
			l.vars.disableCompletion, _ = v.(bool)
		case "show-all-if-ambiguous":
			l.vars.showAllIfAmbiguous, _ = v.(bool)
		case "show-all-if-unmodified":
			l.vars.showAllIfUnmodified, _ = v.(bool)
		case "completion-query-items":
			if n, ok := v.(int); ok {
				l.vars.queryItems = n
			}
		case "isearch-terminators":
			// The value is a key sequence as an inputrc file writes one, such
			// as \C-a, and readline reads it as one.
			if s, ok := v.(string); ok && s != "" {
				l.vars.isearchTerminators = nil
				for _, r := range inputrc.Unescape(s) {
					l.vars.isearchTerminators = append(l.vars.isearchTerminators, key.Code(r))
				}
			}
		case "editing-mode":
			l.SetEditingMode(v == "vi")
		case "comment-begin":
			if s, ok := v.(string); ok {
				l.vars.commentBegin = s
			}
		default:
			unknown = append(unknown, name)
		}
	}
	return unknown
}
