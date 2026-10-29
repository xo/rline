package readline

import "github.com/xo/rline/key"

// Keymap maps sequences of keys to the names of commands.
type Keymap struct {
	root node
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
