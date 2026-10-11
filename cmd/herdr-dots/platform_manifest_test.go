package main

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"herdr-dots/internal/version"
)

// platformManifest is the part of herdr-plugin.toml that decides where and
// how the plugin builds and runs.
type platformManifest struct {
	Version   string   `toml:"version"`
	Platforms []string `toml:"platforms"`
	Build     []struct {
		Command   []string `toml:"command"`
		Platforms []string `toml:"platforms"`
	} `toml:"build"`
	Startup []struct {
		Command   []string `toml:"command"`
		Platforms []string `toml:"platforms"`
	} `toml:"startup"`
	Actions []struct {
		ID        string   `toml:"id"`
		Command   []string `toml:"command"`
		Platforms []string `toml:"platforms"`
	} `toml:"actions"`
	Panes []struct {
		ID        string   `toml:"id"`
		Command   []string `toml:"command"`
		Platforms []string `toml:"platforms"`
	} `toml:"panes"`
}

func readPlatformManifest(t *testing.T) platformManifest {
	t.Helper()
	var m platformManifest
	if _, err := toml.DecodeFile("../../herdr-plugin.toml", &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestManifestDeclaresEveryPlatformAndOneBuildEach(t *testing.T) {
	m := readPlatformManifest(t)
	if !slices.Equal(m.Platforms, []string{"macos", "linux", "windows"}) {
		t.Fatalf("platforms %v", m.Platforms)
	}
	if m.Version != version.Version {
		t.Fatalf("manifest version %s, binary %s", m.Version, version.Version)
	}
	for _, goos := range m.Platforms {
		var builds [][]string
		for _, b := range m.Build {
			if len(b.Platforms) == 0 || slices.Contains(b.Platforms, goos) {
				builds = append(builds, b.Command)
			}
		}
		if len(builds) != 1 {
			t.Fatalf("%s gets %d build steps: %v", goos, len(builds), builds)
		}
		want := []string{"sh", "scripts/build.sh"}
		if goos == "windows" {
			want = []string{"powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", "scripts/build.ps1"}
		}
		if !slices.Equal(builds[0], want) {
			t.Errorf("%s build %v, want %v", goos, builds[0], want)
		}
	}
}

// Herdr rejects duplicate action and pane ids even when their platforms
// differ, so every runtime command must work on every OS: ./bin/herdr-dots
// resolves to bin\herdr-dots.exe on Windows.
func TestRuntimeCommandsAreSharedByEveryPlatform(t *testing.T) {
	m := readPlatformManifest(t)
	check := func(kind, id string, cmd, platforms []string) {
		if len(platforms) != 0 {
			t.Errorf("%s %q must not narrow platforms (%v)", kind, id, platforms)
		}
		if len(cmd) == 0 || cmd[0] != "./bin/herdr-dots" {
			t.Errorf("%s %q runs %v, want ./bin/herdr-dots ...", kind, id, cmd)
		}
	}
	for _, s := range m.Startup {
		check("startup", "", s.Command, s.Platforms)
	}
	ids := map[string]bool{}
	for _, a := range m.Actions {
		check("action", a.ID, a.Command, a.Platforms)
		if ids["a:"+a.ID] {
			t.Errorf("duplicate action %s", a.ID)
		}
		ids["a:"+a.ID] = true
	}
	for _, p := range m.Panes {
		check("pane", p.ID, p.Command, p.Platforms)
		if ids["p:"+p.ID] {
			t.Errorf("duplicate pane %s", p.ID)
		}
		ids["p:"+p.ID] = true
	}
}

func TestWindowsBuildScriptBuildsTheExe(t *testing.T) {
	b, err := os.ReadFile("../../scripts/build.ps1")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		"[System.Security.Cryptography.SHA256]::Create()",     // verifies the download against release/SHA256SUMS, with no module
		"[System.IO.Compression.ZipFile]::ExtractToDirectory", // unzips with no module
		"HERDR_PLUGIN_BUILD_OFFLINE",
		`'.\cmd\herdr-dots'`, // the source-build fallback
		"Install Go 1.25 or newer",
		"exit $code",
		"bin/$Name.exe", // the installed binary
		// pwsh is not on every dev machine, so pin the fixes the PS tests
		// exercise where it is: an empty REPO is "" (not $null.Trim()), no
		// progress bar, a validated platform override.
		`"$(Get-Content -Raw -LiteralPath 'release/REPO')".Trim()`,
		"$ProgressPreference = 'SilentlyContinue'",
		"'^(darwin|linux|windows)_(amd64|arm64)$'",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("build.ps1 lacks %q", want)
		}
	}
	// Windows PowerShell started from PowerShell 7 can't load the script
	// modules these live in, so the Windows install failed with them.
	for _, line := range strings.Split(s, "\n") {
		if code, _, _ := strings.Cut(line, "#"); strings.Contains(code, "Get-FileHash") || strings.Contains(code, "Expand-Archive") {
			t.Errorf("build.ps1 calls a script-module cmdlet: %q", strings.TrimSpace(line))
		}
	}
}
