package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"slices"
	"strings"
	"time"

	"golang.org/x/term"

	"herdr-dots/internal/credentials"
	"herdr-dots/internal/paths"
)

// keysIO is everything runKeys touches, so tests can drive it.
type keysIO struct {
	store    credentials.File
	in       io.Reader
	out      io.Writer
	isTTY    func() bool
	readPass func() ([]byte, error) // no-echo read from the terminal
	verify   func(ctx context.Context, provider string) error
}

const keysUsage = "usage: herdr-dots keys set <provider> | remove <provider> | list | test [<provider>]  (providers: anthropic, openai, openrouter; test also takes ollama; test exits non-zero if any provider fails)"

// runKeys manages API keys in CONFIG_DIR/credentials.toml. Keys are read from
// stdin only (no echo on a terminal), never from arguments.
func runKeys(args []string) error {
	if len(args) == 0 {
		return errors.New(keysUsage) // usage needs no plugin state (reconciled)
	}
	p, err := paths.Discover()
	if err != nil {
		return err
	}
	set := newBackendSet(p, configLoader(p.ConfigFile()), nil)
	fd := int(os.Stdin.Fd())
	return keysCmd(args, keysIO{
		store:    set.creds(set.cfg()),
		in:       os.Stdin,
		out:      os.Stdout,
		isTTY:    func() bool { return term.IsTerminal(fd) },
		readPass: func() ([]byte, error) { return term.ReadPassword(fd) },
		verify:   set.TestProvider,
	})
}

// errUnknownProvider never carries the argument: a key typed where the
// provider belongs must not reach the terminal's scrollback.
var errUnknownProvider = fmt.Errorf("unknown provider (want one of: %s)", strings.Join(credentials.Providers, ", "))

func keysCmd(args []string, k keysIO) error {
	if len(args) == 0 {
		return errors.New(keysUsage)
	}
	root, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	switch args[0] {
	case "set":
		if len(args) != 2 {
			if len(args) > 2 {
				return errors.New("pass the key on stdin, never as an argument (it would land in your shell history)")
			}
			return errors.New(keysUsage)
		}
		provider := args[1]
		if !slices.Contains(credentials.Providers, provider) {
			return errUnknownProvider
		}
		key, err := readKey(k, provider)
		if err != nil {
			return err
		}
		if err := k.store.Set(provider, key); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(root, 20*time.Second)
		defer cancel()
		if err := k.verify(ctx, provider); err != nil {
			fmt.Fprintf(k.out, "stored · not verified: %s\n", credentials.Redact(err.Error(), key))
			return nil
		}
		fmt.Fprintln(k.out, "stored · verified")
		return nil
	case "remove":
		if len(args) != 2 {
			return errors.New(keysUsage)
		}
		if !slices.Contains(credentials.Providers, args[1]) {
			return errUnknownProvider
		}
		if err := k.store.Remove(args[1]); err != nil {
			return err
		}
		fmt.Fprintln(k.out, "removed "+args[1])
		return nil
	case "list":
		rows, err := k.store.List()
		if err != nil {
			return err
		}
		for _, r := range rows {
			fmt.Fprintf(k.out, "%-11s %-26s %s\n", r.Provider, r.From, r.Last4)
		}
		return nil
	case "test":
		targets := args[1:]
		if len(targets) == 0 {
			targets = append(append([]string{}, credentials.Providers...), "ollama")
		}
		failed := 0
		for _, prov := range targets {
			if prov != "ollama" && !slices.Contains(credentials.Providers, prov) {
				failed++
				fmt.Fprintf(k.out, "%-11s ✗ %s\n", "(unknown)", errUnknownProvider)
				continue
			}
			ctx, cancel := context.WithTimeout(root, 20*time.Second)
			err := k.verify(ctx, prov)
			cancel()
			if err != nil {
				failed++
				fmt.Fprintf(k.out, "%-11s ✗ %s\n", prov, credentials.Redact(err.Error()))
				continue
			}
			fmt.Fprintf(k.out, "%-11s ✓\n", prov)
		}
		if failed > 0 {
			return errors.New("test failed")
		}
		return nil
	}
	return errors.New(keysUsage)
}

// readKey prompts on stderr and reads without echo on a terminal, or one
// line from a pipe (`pbpaste | herdr-dots keys set openai`).
func readKey(k keysIO, provider string) (string, error) {
	if k.isTTY() {
		fmt.Fprintf(os.Stderr, "API key for %s (input hidden): ", provider)
		b, err := k.readPass()
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(b)), nil
	}
	line, err := bufio.NewReader(k.in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	if line = strings.TrimSpace(line); line == "" {
		return "", errors.New("no key on stdin")
	}
	return line, nil
}
