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

var commands = map[string]handler{
	"version":    printVersion,
	"--version":  printVersion,
	"engine":     runEngine,
	"pane":       runPane,
	"manage":     runManage,
	"summon-ui":  runSummonUI,
	"action":     runAction,
	"setup-keys": runSetupKeys,
	"setup-mcp":  runSetupMCP,
	"memory":     runMemory,
	"dots":       runDots,
	"diff-ui":    runDiffUI,
	"keys":       runKeys,
	"mcp":        runMCP,
}

// printVersion prints "herdr-dots <version> (<release|source>, commit <id>)".
func printVersion([]string) error {
	fmt.Println(version.String())
	return nil
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
	protectNewerFiles(args[0])
	return h(args[1:])
}

func main() {
	if err := dispatch(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "herdr-dots:", err)
		os.Exit(1)
	}
}
