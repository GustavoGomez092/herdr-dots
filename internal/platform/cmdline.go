package platform

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf16"
)

// IsBatchFile reports whether path names a .cmd or .bat script, which
// Windows runs through cmd.exe (npm installs claude and codex this way).
func IsBatchFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(baseName(path)))
	return ext == ".cmd" || ext == ".bat"
}

// baseName is the last element of a path written with / or \ separators,
// whatever OS runs the code.
func baseName(p string) string {
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[i+1:]
	}
	return p
}

// findExecutable returns the first dirs[i]/name+ext that isFile accepts.
// Relative PATH entries are skipped, so a program in the current directory
// can never shadow the real one.
func findExecutable(name string, dirs []string, ext string, isFile func(string) bool) string {
	for _, d := range dirs {
		if d == "" || !filepath.IsAbs(d) {
			continue
		}
		if c := filepath.Join(d, name+ext); isFile(c) {
			return c
		}
	}
	return ""
}

// quoteBatchArg quotes one argument for a command line that cmd.exe parses
// and then hands to a program using the usual Windows argv rules. Inside
// double quotes cmd.exe treats & | < > ^ ( ) literally; " % ! and line
// breaks can't be made safe, so they are refused. Backslashes before the
// closing quote are doubled so they stay literal. what names the argument in
// the error ("argument 3"); the error never holds the argument's text, which
// may be a prompt or a key.
func quoteBatchArg(what, a string) (string, error) {
	if i := strings.IndexAny(a, "\"%!\r\n\x00"); i >= 0 {
		return "", fmt.Errorf("%w: %s contains %s", ErrUnsafeBatchArg, what, batchCharClass(a[i]))
	}
	trail := len(a) - len(strings.TrimRight(a, `\`))
	return `"` + a + strings.Repeat(`\`, trail) + `"`, nil
}

// batchCharClass names a character quoteBatchArg refuses.
func batchCharClass(c byte) string {
	switch c {
	case '"':
		return "a double quote"
	case '%':
		return "a percent sign"
	case '!':
		return "an exclamation mark"
	case 0:
		return "a NUL character"
	}
	return "a line break"
}

// BatchCmdLine builds the command line that runs a .cmd/.bat script through
// cmd.exe with every argument quoted:
//
//	<comspec> /d /s /c ""<script>" "<arg1>" "<arg2>""
//
// /d skips AutoRun commands; /s makes cmd.exe strip only the outer quotes.
// It fails with ErrUnsafeBatchArg or, above MaxBatchCmdLine UTF-16 code units,
// ErrCmdTooLong with a hint naming the script.
func BatchCmdLine(comspec, script string, args []string) (string, error) {
	var b strings.Builder
	if strings.ContainsAny(comspec, " \t") {
		b.WriteString(`"` + comspec + `"`)
	} else {
		b.WriteString(comspec)
	}
	b.WriteString(` /d /s /c "`)
	q, err := quoteBatchArg("the program path", script)
	if err != nil {
		return "", err
	}
	b.WriteString(q)
	for i, a := range args {
		q, err := quoteBatchArg(fmt.Sprintf("argument %d", i+1), a)
		if err != nil {
			return "", err
		}
		b.WriteString(" " + q)
	}
	b.WriteString(`"`)
	line := b.String()
	if n := len(utf16.Encode([]rune(line))); n > MaxBatchCmdLine {
		name := baseName(script)
		stem := strings.TrimSuffix(name, filepath.Ext(name))
		return "", fmt.Errorf("%w for %s (%d > %d): install the native %s.exe or shorten the dot's backend args",
			ErrCmdTooLong, name, n, MaxBatchCmdLine, stem)
	}
	return line, nil
}
