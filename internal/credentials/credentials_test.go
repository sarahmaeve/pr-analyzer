package credentials_test

import (
	"strings"
	"testing"

	"github.com/sarahmaeve/pr-analyzer/internal/credentials"
)

// TestGitHub_isSeeded pins the built-in vendor's identity. A regression
// that renamed the env var or the human-readable label would break
// every CLI error message that surfaces "set GITHUB_TOKEN…"; failing
// here is louder than discovering it via a user-facing typo.
func TestGitHub_isSeeded(t *testing.T) {
	t.Parallel()

	if credentials.GitHub.Name != "GitHub" {
		t.Errorf("GitHub.Name = %q, want %q", credentials.GitHub.Name, "GitHub")
	}
	if credentials.GitHub.EnvVar != "GITHUB_TOKEN" {
		t.Errorf("GitHub.EnvVar = %q, want %q", credentials.GitHub.EnvVar, "GITHUB_TOKEN")
	}
}

func TestVendor_Token_readsEnvVar(t *testing.T) {
	// Cannot run in parallel — t.Setenv mutates process-global state.
	t.Setenv("GITHUB_TOKEN", "abc123")
	if got := credentials.GitHub.Token(); got != "abc123" {
		t.Errorf("Token() = %q, want %q", got, "abc123")
	}
}

func TestVendor_Require_nilWhenSet(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "set-to-anything-nonempty")
	if err := credentials.GitHub.Require(); err != nil {
		t.Errorf("Require() = %v, want nil", err)
	}
}

// TestVendor_Require_errorWhenUnset_mentionsBoth checks the error's
// content, not just its existence. The CLI user reads this message and
// needs to know (a) which vendor is involved and (b) which env var to
// set; a regression that drops either is a usability bug.
func TestVendor_Require_errorWhenUnset_mentionsBoth(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	err := credentials.GitHub.Require()
	if err == nil {
		t.Fatal("Require() = nil, want error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "GitHub") {
		t.Errorf("error %q does not name the vendor", msg)
	}
	if !strings.Contains(msg, "GITHUB_TOKEN") {
		t.Errorf("error %q does not name the env var", msg)
	}
}

// TestVendor_isExtensible proves the type is generic over vendor —
// a future GitLab / Bitbucket / corporate-Gerrit vendor can be added
// without code in the credentials package itself. Construct an ad-hoc
// vendor here; the same Token / Require behavior must hold.
func TestVendor_isExtensible(t *testing.T) {
	t.Setenv("MADEUP_TOKEN", "")
	v := credentials.Vendor{Name: "Madeup", EnvVar: "MADEUP_TOKEN"}
	err := v.Require()
	if err == nil {
		t.Fatal("Require() = nil, want error")
	}
	if !strings.Contains(err.Error(), "Madeup") || !strings.Contains(err.Error(), "MADEUP_TOKEN") {
		t.Errorf("error %q does not surface vendor identity", err)
	}

	t.Setenv("MADEUP_TOKEN", "x")
	if err := v.Require(); err != nil {
		t.Errorf("Require() with env set = %v, want nil", err)
	}
}
