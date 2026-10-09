package backend

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"

	"herdr-dots/internal/dots"
)

// CodexCLI runs `codex exec --json` in a read-only sandbox. Codex has no
// system-prompt flag, so system and user prompts go together on stdin.
// NOTE: output format from Codex's docs; not yet live-verified (V4).
type CodexCLI struct {
	Runner  Runner
	Bin     string
	Environ func() []string
}

func (c CodexCLI) Kind() string { return "codex-cli" }

func (c CodexCLI) bin() string {
	if c.Bin != "" {
		return c.Bin
	}
	return "codex"
}

func (c CodexCLI) environ() []string {
	if c.Environ != nil {
		return c.Environ()
	}
	return os.Environ()
}

func (c CodexCLI) Check(context.Context) error {
	if _, err := exec.LookPath(c.bin()); err != nil {
		return fmt.Errorf("codex CLI not found on PATH: install it with `npm i -g @openai/codex`")
	}
	return nil
}

func (c CodexCLI) BuildCmd(r Request) Cmd {
	args := []string{"exec", "--json", "--sandbox", "read-only", "--skip-git-repo-check"}
	if r.Cwd != "" {
		args = append(args, "-C", r.Cwd)
	}
	if r.Model != "" {
		args = append(args, "-m", r.Model)
	}
	args = append(args, dots.SafeArgs(c.Kind(), r.Args)...) // never override --sandbox
	args = append(args, "-")
	return Cmd{Name: c.bin(), Args: args, Dir: r.Cwd, Env: CleanEnv(c.environ()), Stdin: SystemPrompt(r) + "\n\n" + UserPrompt(r)}
}

func (c CodexCLI) Run(ctx context.Context, r Request) (Result, error) {
	if err := checkArgs(c.Kind(), r.Args); err != nil {
		return Result{}, err
	}
	out, stderr, err := c.Runner.Run(ctx, c.BuildCmd(r))
	if ctx.Err() != nil {
		return Result{}, fmt.Errorf("codex timed out: %w", ctx.Err())
	}
	res, perr := ParseCodexOutput(out)
	if err != nil && perr != nil {
		return Result{}, fmt.Errorf("codex failed: %s", Hint(c.Kind(), string(stderr)+"\n"+string(out)))
	}
	return res, perr
}

func ParseCodexOutput(out []byte) (Result, error) {
	var res Result
	var last, failure string
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 0, 1<<20), 16<<20)
	for sc.Scan() {
		var ev struct {
			Type    string `json:"type"`
			Message string `json:"message"`
			Item    struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"item"`
			Usage struct {
				In  int `json:"input_tokens"`
				Out int `json:"output_tokens"`
			} `json:"usage"`
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(sc.Bytes(), &ev) != nil {
			continue
		}
		switch ev.Type {
		case "item.completed":
			if ev.Item.Type == "agent_message" {
				last = ev.Item.Text
			}
		case "turn.completed":
			res.InputTokens, res.OutputTokens = ev.Usage.In, ev.Usage.Out
		case "turn.failed", "error":
			failure = ev.Error.Message
			if failure == "" {
				failure = ev.Message
			}
		}
	}
	if last == "" {
		if failure != "" {
			return res, fmt.Errorf("codex error: %s", failure)
		}
		return res, errors.New("codex produced no agent message")
	}
	res.Raw = last
	v, err := ParseVerdict(last)
	res.Verdict = v
	return res, err
}
