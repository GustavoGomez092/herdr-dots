package ipc

import (
	"encoding/json"
	"testing"
)

func TestRememberArgsWireFormat(t *testing.T) {
	b, _ := json.Marshal(RememberArgs{CardID: "7", As: "rule", Reason: "internal tool"})
	if string(b) != `{"card_id":"7","as":"rule","reason":"internal tool"}` {
		t.Fatalf("%s", b)
	}
	b, _ = json.Marshal(RememberArgs{CardID: "7", As: "finding"})
	if string(b) != `{"card_id":"7","as":"finding"}` {
		t.Fatalf("%s", b)
	}
	if CmdRemember != "remember" || CmdMemoryChanged != "memory_changed" {
		t.Fatal("command names are part of the IPC contract")
	}
}
