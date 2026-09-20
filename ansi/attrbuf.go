// A buffer of attributes, one per byte of the text it describes.
//
// Ported from isocline/src/attr.c.

package ansi

import "slices"

// AttrBuf holds one attribute for every byte of the text it describes. The
// edit loop builds one of these beside the line it is about to draw.
//
// It is a slice of [Attr] and little else, and the methods that are only a
// slice operation are left to the standard library: take [AttrBuf.Extend] and
// use slices.Clone or slices.Equal on what it hands back. What the type adds
// is three things a plain slice does not do.
//
// A nil AttrBuf is usable, and every method accepts one. The C code passes a
// null pointer where a caller wants the text without the attributes, and so
// does this: rline passes nil down through ten function signatures rather
// than branching at each one. That is what the type is mostly for.
//
// Writing past the end extends the buffer rather than panicking, because the
// caller knows the length of the text and not of this. Note that a slice's
// length is what grows: slices.Grow reserves capacity and would not do.
//
// [AttrBuf.UpdateAt] lays an attribute over what is there with [Attr.Merge],
// rather than replacing it.
//
// The C version carries its own capacity and growth policy. A Go slice grows
// on demand, so that is gone.
type AttrBuf struct {
	attrs []Attr
}

// Length returns how many attributes the buffer holds.
func (ab *AttrBuf) Length() int {
	if ab == nil {
		return 0
	}
	return len(ab.attrs)
}

// Clear drops every attribute.
func (ab *AttrBuf) Clear() {
	if ab == nil {
		return
	}
	ab.attrs = ab.attrs[:0]
}

// grow extends the buffer to n attributes, filling any new one with nothing.
//
// This is a length, not a capacity, so slices.Grow is not what it wants.
func (ab *AttrBuf) grow(n int) {
	if n > len(ab.attrs) {
		ab.attrs = append(ab.attrs, make([]Attr, n-len(ab.attrs))...)
	}
}

// Extend returns the attributes, extended to at least n of them. The result
// is the buffer's own slice, so writing to it writes through, and it stops
// being the buffer's as soon as the buffer grows again.
func (ab *AttrBuf) Extend(n int) []Attr {
	if ab == nil {
		return nil
	}
	ab.grow(n)
	return ab.attrs
}

// At returns the attribute at pos, or nothing when pos is outside the buffer.
//
// The C code tests pos against the count with the wrong comparison, so at the
// one position just past the end it reads a slot it never wrote. That is
// undefined behavior rather than a wrong answer, so there is nothing to
// reproduce. This returns the empty attribute there.
func (ab *AttrBuf) At(pos int) Attr {
	if ab == nil || pos < 0 || pos >= len(ab.attrs) {
		return Attr{}
	}
	return ab.attrs[pos]
}

// SetAt replaces count attributes from pos, extending the buffer if it has to.
func (ab *AttrBuf) SetAt(pos, count int, a Attr) {
	ab.fill(pos, count, a, false)
}

// UpdateAt lays a over count attributes from pos, extending the buffer if it
// has to.
func (ab *AttrBuf) UpdateAt(pos, count int, a Attr) {
	ab.fill(pos, count, a, true)
}

// fill is the shared part of SetAt and UpdateAt.
func (ab *AttrBuf) fill(pos, count int, a Attr, update bool) {
	if ab == nil || pos < 0 || count <= 0 {
		return
	}
	end := pos + count
	ab.grow(end)
	for i := pos; i < end; i++ {
		if update {
			ab.attrs[i] = ab.attrs[i].Merge(a)
			continue
		}
		ab.attrs[i] = a
	}
}

// InsertAt makes room for count attributes at pos and fills them with a.
func (ab *AttrBuf) InsertAt(pos, count int, a Attr) {
	if ab == nil || pos < 0 || pos > len(ab.attrs) || count <= 0 {
		return
	}
	ab.attrs = slices.Insert(ab.attrs, pos, slices.Repeat([]Attr{a}, count)...)
}

// DeleteAt removes count attributes from pos. It removes fewer when the
// buffer ends first.
//
// The C code hands an attribute count to memmove where every other function
// hands it a byte count, so it shifts one eighth of what it should and leaves
// stale attributes behind. bbcode.c calls this, so the fault is live rather
// than dead code. The port does the intended thing instead, because the bytes
// that the fault leaves behind depend on how a compiler packs a bit field,
// which makes the wrong answer unportable and therefore useless as a
// reference. testdata/attr-delta.txt records what the C does.
func (ab *AttrBuf) DeleteAt(pos, count int) {
	if ab == nil || pos < 0 || pos > len(ab.attrs) {
		return
	}
	if pos+count > len(ab.attrs) {
		count = len(ab.attrs) - pos
	}
	if count <= 0 {
		return
	}
	ab.attrs = slices.Delete(ab.attrs, pos, pos+count)
}
