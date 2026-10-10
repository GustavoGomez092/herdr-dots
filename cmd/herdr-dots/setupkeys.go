package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"herdr-dots/internal/keys"
	"herdr-dots/internal/platform"
)

func runSetupKeys(args []string) error {
	fs := flag.NewFlagSet("setup-keys", flag.ContinueOnError)
	yes := fs.Bool("yes", false, "append without asking")
	if err := fs.Parse(args); err != nil {
		return err
	}
	home, _ := os.UserHomeDir()
	path := keys.ConfigPath(os.Getenv, home)
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	existing, err := keys.Existing(data)
	if err != nil {
		return fmt.Errorf("%s is not valid TOML: %w", path, err)
	}
	add, already, conflicts := keys.Plan(keys.Defaults(), existing)
	for _, b := range already {
		fmt.Printf("✓ %s already runs %s\n", b.Key, b.Action)
	}
	for _, c := range conflicts {
		fmt.Printf("✗ %s is taken by %s; skipping %s (bind it yourself to another key)\n", c.Binding.Key, c.With, c.Binding.Action)
	}
	if len(add) == 0 {
		fmt.Println("Nothing to add.")
		return nil
	}
	fmt.Printf("\nThis will be appended to %s:\n%s\n", path, keys.Block(add))
	if !*yes {
		fmt.Print("Append it? [y/N] ")
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if strings.ToLower(strings.TrimSpace(line)) != "y" {
			fmt.Println("Cancelled. Nothing changed.")
			return nil
		}
	}
	backup, err := keys.Append(path, add, time.Now())
	if err != nil {
		return err
	}
	fmt.Println(appendedLine(backup))
	bin := os.Getenv("HERDR_BIN_PATH")
	if bin == "" {
		bin = "herdr"
	}
	if out, err := reloadHerdrConfig(bin); err != nil {
		fmt.Printf("Run `herdr server reload-config` yourself (%v: %s)\n", err, strings.TrimSpace(string(out)))
	} else {
		fmt.Println("Herdr config reloaded.")
	}
	return nil
}

// appendedLine reports where the original config was saved ("" when there
// was no config yet, so nothing to back up).
func appendedLine(backup string) string {
	if backup == "" {
		return "Appended. There was no config to back up."
	}
	return "Appended. Backup: " + backup
}

// reloadHerdrConfig runs `<bin> server reload-config` with the child
// environment every product child gets (platform.ChildEnv).
func reloadHerdrConfig(bin string) ([]byte, error) {
	cmd, err := platform.Command(bin, []string{"server", "reload-config"})
	if err != nil {
		return nil, err
	}
	cmd.Env = platform.ChildEnv(os.Environ())
	return cmd.CombinedOutput()
}
