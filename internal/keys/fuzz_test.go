package keys

import "testing"

func FuzzKeysExisting(f *testing.F) {
	f.Add("[keys]\nprefix = \"ctrl+b\"\nsplit = \"prefix+v\"\n")
	f.Add(Block(Defaults()))
	f.Add("[[keys.command]]\nkey = \"prefix+d\"\ncommand = \"herdr-dots.manage\"\n[[keys.command]]\nkey = 3\n")
	f.Add("keys = 5\n")
	f.Add("[keys]\ncommand = [{ key = \"prefix+x\", command = \"a\" }, 7]\n")
	f.Fuzz(func(t *testing.T, data string) {
		m, err := Existing([]byte(data))
		if err != nil {
			return
		}
		for k := range m {
			if k == "" {
				t.Fatalf("empty key bound in %q", data)
			}
		}
		// Planning against any parsed config never panics and never adds a
		// key that is already taken.
		add, _, _ := Plan(Defaults(), m)
		for _, b := range add {
			if _, taken := m[b.Key]; taken {
				t.Fatalf("Plan adds %s, which %q already binds", b.Key, data)
			}
		}
	})
}
