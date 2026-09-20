// Tests for reading a color out of written text.
//
// Nothing here is a replay. The C probe records no case of either function:
// testdata/bbcode.txt holds one "color=" tag, no "bgcolor=", and no
// "ansi-color" or "ansi-sgr" at all. So these are the only thing standing
// behind the reading, and the decimal scan in particular was written out
// again when the code moved into this package.

package ansi

import "testing"

func TestParseColor(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		in   string
		want Code
		why  string
	}{
		{"", None, "nothing names no color"},
		{"none", None, "and so does the word for it"},
		{"red", RGBHex(0xff0000), "a name from the table"},
		{"ansi-red", Red, "an ansi- name gives a palette code, not an RGB value"},
		{"Red", None, "the table is matched exactly, so case matters"},
		{"nosuchcolor", None, "an unknown name is not an error, it is no color"},
		{"#ff0000", RGBHex(0xff0000), "a hex value"},
		{"#FF0000", RGBHex(0xff0000), "hex digits in either case"},
		// The C reads this with sscanf %x, which takes the digits it finds
		// and does not widen a short value the way CSS does.
		{"#f00", RGBHex(0x000f00), "a short hex value is not widened, so this is not red"},
		{"#", None, "a hash with no digits"},
		{"#zz", None, "a hash with no hex digits"},
		{"#ff0000ff", RGBHex(0xff0000ff & 0xFFFFFF), "sscanf keeps the low bits of a longer value"},
		{"#ff0000junk", RGBHex(0xff0000), "and stops at the first byte that is not a digit"},
		// Everything below was measured against gcc on glibc rather than
		// reasoned about. A 64 bit accumulator holds the digits, saturating
		// rather than wrapping, and its low 32 bits are the answer. Found by
		// windows-vm, who noticed scanHex contradicting its own comment.
		{"#123456789", RGBHex(0x23456789), "nine digits give the low bits, not a refusal"},
		{"#100000000", RGBHex(0x000000), "which here is black rather than no color"},
		{"#ffffffffffffffff", RGBHex(0xffffff), "sixteen digits still fit the accumulator"},
		{"#fffffffffffffffff", RGBHex(0xffffff), "seventeen saturate it"},
		{"#123456789abcdef012", RGBHex(0xffffff), "and a saturated value is not its own low bits"},
	} {
		if got := ParseColor(test.in); got != test.want {
			t.Errorf("ParseColor(%q) = %#08x, want %#08x: %s",
				test.in, uint32(got), uint32(test.want), test.why)
		}
	}
}

func TestParseANSI256(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		in   string
		want Code
		why  string
	}{
		{"0", FromANSI256(0), "the first entry"},
		{"5", FromANSI256(5), "a palette code"},
		{"255", FromANSI256(255), "the last real entry"},
		{"256", FromANSI256(256), "the C allows one past the end, and so does this"},
		{"257", None, "past that is no color"},
		{"-1", None, "and so is below zero"},
		{"", None, "nothing is no color"},
		{"red", None, "and neither is a name"},
		// The C reads this with sscanf %d, which skips leading space, takes
		// an optional sign, and ignores whatever follows the digits.
		{" 5", FromANSI256(5), "leading space is skipped, the way sscanf does"},
		{"\t5", FromANSI256(5), "and a tab is space too"},
		{"+5", FromANSI256(5), "a leading plus is allowed"},
		{"5junk", FromANSI256(5), "and the scan stops at the first byte that is not a digit"},
		{"junk5", None, "but it does not search for digits"},
		{"99999999999999999999", None, "a number too large for an int is refused"},
	} {
		if got := ParseANSI256(test.in); got != test.want {
			t.Errorf("ParseANSI256(%q) = %#08x, want %#08x: %s",
				test.in, uint32(got), uint32(test.want), test.why)
		}
	}
}
