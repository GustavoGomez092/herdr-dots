package backend

import (
	"fmt"
	"strings"

	"herdr-dots/internal/transcript"
)

const contract = `RESPONSE FORMAT - reply with ONLY one JSON object, no prose, no code fences:
{"relevant": true or false, "severity": "info" | "warn" | "critical", "message": "<your input>"}
Rules:
- You are read-only. Suggest; never claim to have changed anything.
- Never repeat advice that already appears in the conversation.
- "critical" is for security holes, data loss or broken builds; "warn" for real problems; "info" for improvements.`

const silenceRule = `- If you have nothing important to add from your purpose, reply {"relevant": false, "severity": "info", "message": ""}. Silence is better than noise.`

const forceRule = `- The developer summoned you directly. Always answer with "relevant": true and your best input.`

func verbosityRule(v string) string {
	switch v {
	case "terse":
		return "Keep the message to 1-2 sentences."
	case "detailed":
		return "Be thorough: explain the reasoning and give concrete steps or code."
	default:
		return "Keep the message short: a few sentences or a short list."
	}
}

func SystemPrompt(r Request) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You are %q, a specialist \"dot\" observing a developer's conversation with a coding agent. Contribute only from your expertise.\n\n", r.DotName)
	fmt.Fprintf(&b, "YOUR PURPOSE:\n%s\n\n", strings.TrimSpace(r.Purpose))
	fmt.Fprintf(&b, "%s\n\n", verbosityRule(r.Verbosity))
	b.WriteString(contract + "\n")
	if r.Force {
		b.WriteString(forceRule + "\n")
	} else {
		b.WriteString(silenceRule + "\n")
	}
	return b.String()
}

func UserPrompt(r Request) string {
	var b strings.Builder
	b.WriteString("CONVERSATION (most recent turns):\n\n")
	b.WriteString(transcript.Format(r.Turns))
	if q := strings.TrimSpace(r.Question); q != "" {
		fmt.Fprintf(&b, "\n\nQUESTION FROM THE DEVELOPER:\n%s", q)
	}
	b.WriteString("\n\nRespond with the JSON object only.")
	return b.String()
}
