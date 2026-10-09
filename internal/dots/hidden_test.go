package dots

import "testing"

// HiddenCarriers are invisible characters that can smuggle a byte payload.
// Tests in other packages use it through the exported helper below.
var hiddenCarriers = map[string]string{
	"VS1":              "a︀b",
	"VS16 mid-text":    "a️b",
	"VS text style":    "a︎b",
	"VS supplement":    "a\U000E0100b",
	"VS supp last":     "a\U000E01EFb",
	"CGJ":              "a͏b",
	"Hangul choseong":  "aᅟb",
	"Hangul jungseong": "aᅠb",
	"Hangul filler":    "aㅤb",
	"halfwidth filler": "aﾠb",
	"braille blank":    "a⠀b",
	"Mongolian FVS1":   "a᠋b",
	"Mongolian FVS4":   "a᠏b",
	"Khmer inherent":   "a឴b",
	"interlinear":      "a￹b",
}

func TestUnsafeTextRefusesInvisiblePayloadCarriers(t *testing.T) {
	for name, s := range hiddenCarriers {
		if err := UnsafeText(s, false); err == nil {
			t.Errorf("UnsafeText accepted %s", name)
		}
		d := New("x")
		d.Purpose = s
		if err := d.Validate(); err == nil {
			t.Errorf("purpose accepted %s", name)
		}
	}
}

func TestGlyphKeepsEmojiSequences(t *testing.T) {
	for _, g := range []string{"❤️", "\U0001F468‍\U0001F469️"} {
		if hiddenText(g, true) {
			t.Errorf("glyph %q refused", g)
		}
	}
	if !hiddenText("͏", true) || !hiddenText("︀", true) {
		t.Fatal("glyph accepted a carrier")
	}
}
