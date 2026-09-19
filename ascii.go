package rline

// Case-insensitive comparison, by the ASCII rule only. Only the letters A to Z
// change case. A byte above 0x7f never changes, so "é" and "É" do not match.
//
// This is not what strings.EqualFold does. That function folds by the Unicode
// rules, and it does match "é" against "É".
//
// Ported from isocline/src/common.c.

// asciiLower returns c in lower case, by the ASCII rule only.
func asciiLower(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c - 'A' + 'a'
	}
	return c
}

// compareFold compares a and b without regard to ASCII case. It returns -1, 0
// or 1.
//
// The length decides first. A shorter string sorts before a longer one,
// whatever the two hold, so this is not an alphabetical order. "b" sorts
// before "aa". Strings of equal length compare byte by byte, with the signed
// order that compareFoldN describes.
func compareFold(a, b string) int {
	switch {
	case len(a) < len(b):
		return -1
	case len(a) > len(b):
		return 1
	}
	return compareFoldN(a, b, len(a))
}

// compareFoldN compares the first n bytes of a and b without regard to ASCII
// case. It returns -1, 0 or 1.
//
// The bytes compare as signed values, so any byte above 0x7f sorts before
// every ASCII character. "a" is greater than "\xff", not less. The C code
// compares C char values, and char is signed on x86 and on x86-64, which is
// where the recorded corpus comes from.
//
// This makes the C code sort differently on a machine where char is unsigned,
// which is the default on ARM. The port keeps the x86 order, because that is
// what the corpus records. See the note in PLAN.md.
//
// When a runs out before n bytes and b keeps going, the result is -1.
func compareFoldN(a, b string, n int) int {
	i := 0
	for ; i < len(a) && i < n; i++ {
		c1 := int8(asciiLower(a[i]))
		var c2 int8
		if i < len(b) {
			c2 = int8(asciiLower(b[i]))
		}
		switch {
		case c1 < c2:
			return -1
		case c1 > c2:
			return 1
		}
	}
	if i >= n || i >= len(b) {
		return 0
	}
	return -1
}

// hasPrefixFold reports whether s starts with prefix, without regard to ASCII
// case.
func hasPrefixFold(s, prefix string) bool {
	i := 0
	for ; i < len(s) && i < len(prefix); i++ {
		if asciiLower(s[i]) != asciiLower(prefix[i]) {
			return false
		}
	}
	return i >= len(prefix)
}

// indexFold returns the position of the first substr in s, without regard to
// ASCII case, or -1 when s does not hold it. An empty substr returns 0.
func indexFold(s, substr string) int {
	if substr == "" {
		return 0
	}
	for i := range len(s) {
		if compareFoldN(s[i:], substr, len(substr)) == 0 {
			return i
		}
	}
	return -1
}

// containsFold reports whether s holds substr, without regard to ASCII case.
func containsFold(s, substr string) bool {
	return indexFold(s, substr) >= 0
}
