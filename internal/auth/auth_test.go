package auth

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseProfileRef(t *testing.T) {
	tests := []struct {
		in          string
		wantBrowser string
		wantProfile string
	}{
		{"", "", ""},
		{"firefox", "firefox", ""},
		{"firefox/Default (release)", "firefox", "Default (release)"},
		{" firefox / work ", "firefox", "work"},
		{"chrome/", "chrome", ""},
	}
	for _, tc := range tests {
		b, p := ParseProfileRef(tc.in)
		if b != tc.wantBrowser || p != tc.wantProfile {
			t.Errorf("ParseProfileRef(%q) = (%q,%q), want (%q,%q)", tc.in, b, p, tc.wantBrowser, tc.wantProfile)
		}
	}
}

func TestMatchProfile(t *testing.T) {
	tests := []struct {
		want, got string
		match     bool
	}{
		{"Default (release)", "Default (release)", true},
		{"default (RELEASE)", "Default (release)", true},
		{"  Default (release) ", "Default (release)", true},
		{"Default (release)", "/home/u/.mozilla/Default (release)", true},
		{"Default", "Default (release)", false},
		{"", "", true},
		{"work", "", false},
		{"", "work", false},
	}
	for _, tc := range tests {
		if got := matchProfile(tc.want, tc.got); got != tc.match {
			t.Errorf("matchProfile(%q,%q) = %v, want %v", tc.want, tc.got, got, tc.match)
		}
	}
}

func TestProfileLabel(t *testing.T) {
	if got := (Profile{Browser: "firefox"}).Label(); got != "firefox" {
		t.Errorf("Label = %q, want firefox", got)
	}
	if got := (Profile{Browser: "firefox", Profile: "Default"}).Label(); got != "firefox · Default" {
		t.Errorf("Label = %q", got)
	}
}

func TestIsConfigured_readsAuthFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if IsConfigured() {
		t.Error("no auth file should mean not configured")
	}
	if err := SaveCookie("SAPISID=abc; HSID=def", "firefox", "Default (release)"); err != nil {
		t.Fatalf("SaveCookie: %v", err)
	}
	if !IsConfigured() {
		t.Error("a saved cookie should mean configured")
	}
	c, err := Cookie()
	if err != nil || c == "" {
		t.Fatalf("Cookie = %q, %v", c, err)
	}
	// Re-saving with a new profile overwrites cleanly.
	if err := SaveCookie("SAPISID=xyz", "firefox", "work"); err != nil {
		t.Fatalf("SaveCookie update: %v", err)
	}
	if c, _ := Cookie(); c != "SAPISID=xyz" {
		t.Errorf("Cookie after update = %q", c)
	}
}

func TestFirefoxProfileDBs_XDGLibreWolf(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	write := func(rel string) string {
		path := filepath.Join(home, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("db"), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	lw := write(".config/librewolf/librewolf/abc.default-release/cookies.sqlite")
	ff := write(".config/mozilla/firefox/xyz.default-release/cookies.sqlite")

	got := map[string]firefoxProfileDB{}
	for _, db := range firefoxProfileDBs() {
		got[db.path] = db
	}
	if db, ok := got[lw]; !ok {
		t.Errorf("XDG LibreWolf profile not found in %v", got)
	} else if db.browser != "librewolf" || db.profile != "abc.default-release" {
		t.Errorf("LibreWolf db = %+v", db)
	}
	if db, ok := got[ff]; !ok {
		t.Errorf("XDG Firefox profile not found in %v", got)
	} else if db.browser != "firefox" || db.profile != "xyz.default-release" {
		t.Errorf("Firefox db = %+v", db)
	}
}
