package notify

import "strings"

// quoteAppleScript renders s as an AppleScript string literal. AppleScript
// escapes backslashes and double quotes with a backslash, and the script is
// passed as a single -e argument, so embedded newlines are folded into spaces
// to keep it on one line.
func quoteAppleScript(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(s)
	return `"` + s + `"`
}
