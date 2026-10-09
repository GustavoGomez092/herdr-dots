package memory

import "testing"

func TestImportPackRefusesInvisibleCarriers(t *testing.T) {
	s := testStore(t)
	for _, c := range []string{"︀", "\U000E0100", "͏", "ᅟ", "ᅠ", "ㅤ", "ﾠ", "⠀", "᠋", "᠏", "️", "‍"} {
		res, err := s.ImportPack(pid, "security", []Entry{{Scope: ScopeProject, Kind: KindNote, Text: "heart " + c + " x"}})
		if err != nil || res.Added != 0 || len(res.Rejected) != 1 {
			t.Errorf("%q accepted: %+v %v", c, res, err)
		}
	}
	res, err := s.ImportPack(pid, "security", []Entry{{Scope: ScopeProject, Kind: KindNote, Text: "plain note"}})
	if err != nil || res.Added != 1 {
		t.Fatalf("plain: %+v %v", res, err)
	}
}
