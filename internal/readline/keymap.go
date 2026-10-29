package readline

import "github.com/xo/rline/key"

// Keymap maps sequences of keys to the names of commands.
type Keymap struct {
	root node

	// sources are the key sequences that bindings were made from, as a
	// terminal sends them, by the sequence, so that dump-functions and
	// dump-macros can print them as readline does.
	sources map[string]source
}

// source is one binding as it was made: the keys that its sequence decodes
// to, and the text of a macro.
type source struct {
	keys  []key.Code
	macro string
}

// BindSeq binds keys, which the key sequence seq decodes to, to the command
// called name, as Bind does, and keeps seq for dump-functions.
func (k *Keymap) BindSeq(seq string, keys []key.Code, name string) {
	k.Bind(keys, name)
	k.keep(seq, source{keys: keys})
}

// BindMacroSeq binds keys, which the key sequence seq decodes to, to the
// macro whose text is text and whose keys are macro, as BindMacro does, and
// keeps both texts for dump-macros.
func (k *Keymap) BindMacroSeq(seq string, keys []key.Code, text string, macro []key.Code) {
	k.BindMacro(keys, macro)
	k.keep(seq, source{keys: keys, macro: text})
}

// keep records the binding that seq made.
func (k *Keymap) keep(seq string, s source) {
	if k.sources == nil {
		k.sources = make(map[string]source)
	}
	s.keys = append([]key.Code(nil), s.keys...)
	k.sources[seq] = s
}

// node is one key of a sequence: the command or the macro bound to the keys
// up to it, and the keys that can follow.
type node struct {
	name  string
	macro []key.Code
	next  map[key.Code]*node
}

// NewKeymap returns a keymap that binds nothing.
func NewKeymap() *Keymap { return &Keymap{} }

// Bind binds the sequence seq to the command called name, replacing what it
// was bound to.
func (k *Keymap) Bind(seq []key.Code, name string) {
	n := k.node(seq)
	n.name, n.macro = name, nil
}

// BindMacro binds the sequence seq to a macro: the keys that it stands for,
// which run as though they were typed.
func (k *Keymap) BindMacro(seq, keys []key.Code) {
	n := k.node(seq)
	n.name, n.macro = "", append([]key.Code(nil), keys...)
}

// node returns the node of seq, and makes the nodes on the way to it.
func (k *Keymap) node(seq []key.Code) *node {
	n := &k.root
	for _, c := range seq {
		if n.next == nil {
			n.next = make(map[key.Code]*node)
		}
		child, ok := n.next[c]
		if !ok {
			child = &node{}
			n.next[c] = child
		}
		n = child
	}
	return n
}

// lookup finds seq in the keymap. It returns the name of the command bound to
// it, or reports that seq is the start of a longer sequence. A sequence that
// is neither is not bound.
//
// A sequence that is bound and also starts a longer one waits for the next
// key. readline decides between the two after keyseq-timeout, which comes
// with the keymaps in phase 2 of D43.
func (k *Keymap) lookup(seq []key.Code) (name string, prefix bool) {
	n, prefix := k.find(seq)
	if n == nil {
		return "", prefix
	}
	return n.name, false
}

// boundPrefix returns the longest start of seq, shorter than seq, that is
// bound to a command or a macro, and its node. It returns 0 when none is.
func (k *Keymap) boundPrefix(seq []key.Code) (int, *node) {
	n, best, bestNode := &k.root, 0, (*node)(nil)
	for i, c := range seq[:len(seq)-1] {
		child, ok := n.next[c]
		if !ok {
			break
		}
		n = child
		if n.name != "" || n.macro != nil {
			best, bestNode = i+1, n
		}
	}
	return best, bestNode
}

// find returns the node of seq when seq is bound, or reports that it is the
// start of a longer sequence.
func (k *Keymap) find(seq []key.Code) (*node, bool) {
	n := &k.root
	for _, c := range seq {
		child, ok := n.next[c]
		if !ok {
			return nil, false
		}
		n = child
	}
	if len(n.next) > 0 {
		return nil, true
	}
	return n, false
}
