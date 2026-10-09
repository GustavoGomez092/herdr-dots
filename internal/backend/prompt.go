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

// memoryContract is appended to the system prompt when Request.Learn is set.
const memoryContract = `MEMORY (optional): if this conversation reveals a durable fact worth remembering for future
reviews of this project (a convention, a decision, a recurring risk), add
"remember": [{"op":"add","scope":"dot"|"project","kind":"convention"|"decision"|"finding"|"note",
"text":"<one sentence>","paths":["<glob>"]}] to your JSON (at most 3 items).
To correct a memory: {"op":"update","id":"d:m9","text":"…"}. To drop a wrong one: {"op":"forget","id":"d:m9"}.
Never store secrets, credentials, personal data, or code longer than one line.`

// memoryPassContract replaces the verdict contract for Pass == PassMemory.
const memoryPassContract = `RESPONSE FORMAT - you are not reviewing. Read the conversation and reply with ONLY {"remember":[…]}
(at most 5 items), or {"remember":[]} if nothing durable happened. No prose, no code fences. Each item:
{"op":"add","scope":"dot"|"project","kind":"convention"|"decision"|"finding"|"note","text":"<one sentence>","paths":["<glob>"]}
To correct a memory: {"op":"update","id":"d:m9","text":"…"}. To drop a wrong one: {"op":"forget","id":"d:m9"}.
Remember only durable facts about this project: conventions, decisions, recurring risks.
Never store secrets, credentials, personal data, or code longer than one line.`

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
	if r.Pass == PassMemory {
		b.WriteString(memoryPassContract + "\n")
		return b.String()
	}
	fmt.Fprintf(&b, "%s\n\n", verbosityRule(r.Verbosity))
	b.WriteString(contract + "\n")
	if r.Force {
		b.WriteString(forceRule + "\n")
	} else {
		b.WriteString(silenceRule + "\n")
	}
	if r.Learn {
		b.WriteString("\n" + memoryContract + "\n")
	}
	return b.String()
}

// MemoryPrompt is the memory block that opens the user prompt ("" for none).
// The engine renders it with memory.Render: reference data, never instructions.
func MemoryPrompt(r Request) string { return strings.TrimSpace(r.Memory) }

func UserPrompt(r Request) string {
	var b strings.Builder
	if m := MemoryPrompt(r); m != "" {
		b.WriteString(m + "\n\n")
	}
	b.WriteString("CONVERSATION (most recent turns):\n\n")
	b.WriteString(transcript.Format(r.Turns))
	if q := strings.TrimSpace(r.Question); q != "" {
		fmt.Fprintf(&b, "\n\nQUESTION FROM THE DEVELOPER:\n%s", q)
	}
	b.WriteString("\n\nRespond with the JSON object only.")
	return b.String()
}
