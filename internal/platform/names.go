package platform

import "strings"

// windowsDevices are names Windows maps to devices in every directory, with
// or without an extension ("nul.txt" is NUL too).
var windowsDevices = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true, "CONIN$": true, "CONOUT$": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true, "COM6": true, "COM7": true, "COM8": true, "COM9": true, "COM0": true, "COM¹": true, "COM²": true, "COM³": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true, "LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true, "LPT0": true, "LPT¹": true, "LPT²": true, "LPT³": true,
}

// windowsReservedName is ReservedPathName's Windows rule, kept OS-neutral so
// every OS can test it: any ':' (an alternate data stream or a drive), a
// device name as a path element, or an element ending in a dot or a space
// (Windows strips those, so "env." opens "env"). "." and ".." are left to
// the caller's root check.
func windowsReservedName(rel string) bool {
	if strings.Contains(rel, ":") {
		return true
	}
	for _, el := range strings.FieldsFunc(rel, func(r rune) bool { return r == '/' || r == '\\' }) {
		if el == "." || el == ".." {
			continue
		}
		if strings.HasSuffix(el, ".") || strings.HasSuffix(el, " ") {
			return true
		}
		stem, _, _ := strings.Cut(el, ".")
		if windowsDevices[strings.ToUpper(strings.TrimRight(stem, " "))] {
			return true
		}
	}
	return false
}
