package readline

// killRingSize is how many kills the ring keeps, measured as ten: after
// twelve kills, rotating from the newest reaches the tenth and wraps.
const killRingSize = 10

// killRing holds the text that kill commands took out, oldest first.
type killRing struct {
	ring [][]byte
	top  int // the entry that yank inserts
}

// add puts a kill on the ring. When the kill joins the one before it, the
// text goes onto the newest entry: in front of it when it was killed before
// the cursor, after it otherwise.
func (k *killRing) add(text []byte, join, before bool) {
	if join && len(k.ring) > 0 {
		newest := k.ring[len(k.ring)-1]
		if before {
			newest = append(append([]byte(nil), text...), newest...)
		} else {
			newest = append(append([]byte(nil), newest...), text...)
		}
		k.ring[len(k.ring)-1] = newest
		k.top = len(k.ring) - 1
		return
	}
	k.ring = append(k.ring, text)
	if len(k.ring) > killRingSize {
		k.ring = k.ring[len(k.ring)-killRingSize:]
	}
	k.top = len(k.ring) - 1
}

// current returns the entry that yank inserts.
func (k *killRing) current() ([]byte, bool) {
	if len(k.ring) == 0 {
		return nil, false
	}
	return k.ring[k.top], true
}

// rotate moves to the next older entry, wrapping from the oldest to the
// newest.
func (k *killRing) rotate() {
	if len(k.ring) == 0 {
		return
	}
	k.top--
	if k.top < 0 {
		k.top = len(k.ring) - 1
	}
}
