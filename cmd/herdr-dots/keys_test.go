package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"herdr-dots/internal/config"
	"herdr-dots/internal/credentials"
	"herdr-dots/internal/paths"
)

func TestKeysNeverEchoAnUnrecognisedProviderArgument(t *testing.T) {
	const leak = "sk-proj-REALKEY0123456789abcd"
	for _, args := range [][]string{{"set", leak}, {"test", leak}, {"remove", leak}} {
		k, out, _ := keysRig(t, leak+"\n", nil)
		k.verify = (&backendSet{p: paths.New(t.TempDir(), t.TempDir(), "/tmp/h.sock"), cfg: func() config.Config { return config.Default() }, cache: map[string]cachedBackend{}}).TestProvider
		err := keysCmd(args, k)
		text := out.String()
		if err != nil {
			text += err.Error()
		}
		if strings.Contains(text, "REALKEY") {
			t.Errorf("%v echoed the argument: %q", args, text)
		}
		if err == nil {
			t.Errorf("%v: expected an error", args)
		}
	}
}

func TestKeysTestExitsNonZeroWhenAnyProviderFails(t *testing.T) {
	k, out, _ := keysRig(t, "", nil)
	n := 0
	k.verify = func(context.Context, string) error {
		n++
		if n == 2 {
			return errors.New("boom")
		}
		return nil
	}
	if err := keysCmd([]string{"test"}, k); err == nil {
		t.Fatalf("one failure among all providers must fail the command: %q", out)
	}
	if !strings.Contains(keysUsage, "exit") {
		t.Fatal("usage must document the exit code")
	}
}

func keysRig(t *testing.T, stdin string, verifyErr error) (keysIO, *bytes.Buffer, *[]string) {
	t.Helper()
	var out bytes.Buffer
	var verified []string
	return keysIO{
		store:    credentials.File{Path: filepath.Join(t.TempDir(), "credentials.toml"), Env: func(string) string { return "" }},
		in:       strings.NewReader(stdin),
		out:      &out,
		isTTY:    func() bool { return false },
		readPass: func() ([]byte, error) { t.Fatal("no terminal in tests"); return nil, nil },
		verify: func(_ context.Context, p string) error {
			verified = append(verified, p)
			return verifyErr
		},
	}, &out, &verified
}

func TestKeysSetReadsStdinWritesOwnerOnlyAndVerifies(t *testing.T) {
	k, out, verified := keysRig(t, "sk-ant-api03-PIPEDKEY00001234\n", nil)
	if err := keysCmd([]string{"set", "anthropic"}, k); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(k.store.Path)
	if st.Mode().Perm() != 0o600 || out.String() != "stored · verified\n" || len(*verified) != 1 {
		t.Fatalf("mode %v out %q verified %v", st.Mode().Perm(), out, *verified)
	}
	if key, _, _ := k.store.Lookup("anthropic"); key != "sk-ant-api03-PIPEDKEY00001234" {
		t.Fatalf("stored %q", key)
	}
}

func TestKeysSetUsesTheNoEchoReaderOnATerminal(t *testing.T) {
	k, _, _ := keysRig(t, "", nil)
	k.isTTY = func() bool { return true }
	k.readPass = func() ([]byte, error) { return []byte("sk-or-v1-TTYKEY0123456789\n"), nil }
	if err := keysCmd([]string{"set", "openrouter"}, k); err != nil {
		t.Fatal(err)
	}
	if key, _, _ := k.store.Lookup("openrouter"); key != "sk-or-v1-TTYKEY0123456789" {
		t.Fatalf("stored %q", key)
	}
}

func TestKeysSetRefusesKeyArgumentsAndUnknownProviders(t *testing.T) {
	k, _, _ := keysRig(t, "", nil)
	if err := keysCmd([]string{"set", "openai", "sk-in-argv"}, k); err == nil || !strings.Contains(err.Error(), "never as an argument") {
		t.Fatalf("err = %v", err)
	}
	if err := keysCmd([]string{"set", "ollama"}, k); err == nil {
		t.Fatal("ollama takes no key")
	}
	if err := keysCmd([]string{"set", "openai"}, k); err == nil || !strings.Contains(err.Error(), "no key on stdin") {
		t.Fatalf("empty stdin: %v", err)
	}
}

func TestKeysVerifyFailureIsReportedRedacted(t *testing.T) {
	k, out, _ := keysRig(t, "sk-proj-BADKEY0123456789abcd\n", errors.New("401: Incorrect API key provided: sk-proj-BADKEY0123456789abcd"))
	if err := keysCmd([]string{"set", "openai"}, k); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.String(), "stored · not verified") || strings.Contains(out.String(), "BADKEY") {
		t.Fatalf("out %q", out)
	}
}

func TestKeysListRemoveTest(t *testing.T) {
	k, out, verified := keysRig(t, "sk-ant-api03-LISTKEY000009999\n", nil)
	_ = keysCmd([]string{"set", "anthropic"}, k)
	out.Reset()
	if err := keysCmd([]string{"list"}, k); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "anthropic") || !strings.Contains(out.String(), "…9999") || strings.Contains(out.String(), "LISTKEY") {
		t.Fatalf("list %q", out)
	}
	*verified = nil
	if err := keysCmd([]string{"test"}, k); err != nil || len(*verified) != 4 {
		t.Fatalf("test all: %v %v", err, *verified)
	}
	if err := keysCmd([]string{"remove", "anthropic"}, k); err != nil {
		t.Fatal(err)
	}
	if _, _, err := k.store.Lookup("anthropic"); !errors.Is(err, credentials.ErrNoCredentials) {
		t.Fatal("removed")
	}
	if err := keysCmd(nil, k); err == nil || !strings.Contains(err.Error(), "usage") {
		t.Fatal("usage")
	}
}
