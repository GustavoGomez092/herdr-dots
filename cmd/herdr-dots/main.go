// Command herdr-dots is the single binary behind the Herdr Dots plugin. Herdr
// launches its subcommands from herdr-plugin.toml.
package main

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"herdr-dots/internal/version"
)

type handler func(args []string) error

func notYet(name string) handler {
	return func([]string) error { return fmt.Errorf("%s: not implemented yet", name) }
}

var commands = map[string]handler{
	"version": func([]string) error {
		fmt.Println("herdr-dots", version.Version, "(scaffold)")
		return nil
	},
	"engine":     runEngine,
	"pane":       runPane,
	"manage":     runManage,
	"summon-ui":  runSummonUI,
	"action":     runAction,
	"setup-keys": runSetupKeys,
}

func usage() error {
	names := make([]string, 0, len(commands))
	for n := range commands {
		names = append(names, n)
	}
	sort.Strings(names)
	return errors.New("usage: herdr-dots <" + strings.Join(names, "|") + "> [args]")
}

func dispatch(args []string) error {
	if len(args) == 0 {
		return usage()
	}
	h, ok := commands[args[0]]
	if !ok {
		return fmt.Errorf("unknown command %q (%v)", args[0], usage())
	}
	return h(args[1:])
}

func main() {
	if err := dispatch(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "herdr-dots:", err)
		os.Exit(1)
	}
}
