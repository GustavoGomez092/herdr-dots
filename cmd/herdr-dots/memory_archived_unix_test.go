//go:build unix

package main

import "testing"

func TestMemoryEditSaysWhenTheEntryWasArchivedAtOnce(t *testing.T) {
	c, out, _ := newMemCLI(t)
	e := pinnedImport(t, c)
	out.Reset()
	if err := editWith(t, c, `s/pinned = true/pinned = false/`); err != nil {
		t.Fatal(err)
	}
	wantArchivedMsg(t, out.String(), "saved", e)
}
