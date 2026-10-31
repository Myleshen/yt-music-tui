package app

import "testing"

func contains(args []string, want string) int {
	for i, a := range args {
		if a == want {
			return i
		}
	}
	return -1
}

func hasFastClient(args []string) bool {
	i := contains(args, "--extractor-args")
	return i >= 0 && i+1 < len(args) && args[i+1] == "youtube:player_client="+fastPlayerClient
}

func TestYtdlpAttemptsOrderWithCookies(t *testing.T) {
	const cookies = "/tmp/cookies.txt"
	attempts := ytdlpAttempts(cookies, true)
	if len(attempts) != 3 {
		t.Fatalf("got %d attempts, want 3: %v", len(attempts), attempts)
	}
	// 1) fast client + cookies
	if !hasFastClient(attempts[0]) {
		t.Errorf("attempt 0 should use the fast client: %v", attempts[0])
	}
	if contains(attempts[0], "--cookies") < 0 {
		t.Errorf("attempt 0 should pass cookies: %v", attempts[0])
	}
	// 2) default client + cookies
	if hasFastClient(attempts[1]) {
		t.Errorf("attempt 1 should be the default client: %v", attempts[1])
	}
	if contains(attempts[1], "--cookies") < 0 {
		t.Errorf("attempt 1 should pass cookies: %v", attempts[1])
	}
	// 3) default client, no cookies — the most compatible fallback.
	if hasFastClient(attempts[2]) || contains(attempts[2], "--cookies") >= 0 {
		t.Errorf("attempt 2 should be plain default: %v", attempts[2])
	}
	for i, a := range attempts {
		if contains(a, "-g") < 0 {
			t.Errorf("attempt %d missing -g: %v", i, a)
		}
	}
}

func TestYtdlpAttemptsOrderWithoutCookies(t *testing.T) {
	attempts := ytdlpAttempts("/tmp/cookies.txt", false)
	if len(attempts) != 2 {
		t.Fatalf("got %d attempts, want 2: %v", len(attempts), attempts)
	}
	if !hasFastClient(attempts[0]) {
		t.Errorf("attempt 0 should use the fast client: %v", attempts[0])
	}
	if hasFastClient(attempts[1]) || contains(attempts[1], "--cookies") >= 0 {
		t.Errorf("attempt 1 should be plain default: %v", attempts[1])
	}
}

func TestFastPlayerClientConstant(t *testing.T) {
	if fastPlayerClient != "android" {
		t.Errorf("fastPlayerClient = %q, want android (the measured fastest reliable client)", fastPlayerClient)
	}
}
