package notify

import "testing"

func TestQuoteAppleScript(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"plain", `"plain"`},
		{`say "hi"`, `"say \"hi\""`},
		{`back\slash`, `"back\\slash"`},
		{"two\nlines", `"two lines"`},
		{"crlf\r\nhere", `"crlf here"`},
		{"", `""`},
	} {
		if got := quoteAppleScript(c.in); got != c.want {
			t.Errorf("quoteAppleScript(%q) = %s, want %s", c.in, got, c.want)
		}
	}
}

// TestQuoteAppleScriptEscapesInjection checks that a payload cannot break out of
// the string literal and become a second AppleScript statement.
func TestQuoteAppleScriptEscapesInjection(t *testing.T) {
	got := quoteAppleScript(`x" & do shell script "rm -rf /" & "`)
	want := `"x\" & do shell script \"rm -rf /\" & \""`
	if got != want {
		t.Errorf("quoteAppleScript = %s, want %s", got, want)
	}
}
