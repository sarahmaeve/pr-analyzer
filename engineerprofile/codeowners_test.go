package engineerprofile

import (
	"slices"
	"strings"
	"testing"
)

// A CODEOWNERS exercising the matcher: catch-all, a rooted directory, an
// extension glob, and an exact path — with last-match-wins ordering.
const sampleCodeowners = `# owners
*            @maintainers
/internal/   @alice
*.md         @bob
docs/api.md  @carol
`

func TestParseCodeowners(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		login       string
		changed     []string
		wantPresent bool
		wantIsOwner bool
		wantOwns    bool
		wantOwned   []string
	}{
		{
			name:        "owns via rooted dir (last match wins over catch-all)",
			login:       "alice",
			changed:     []string{"internal/x.go"},
			wantPresent: true, wantIsOwner: true, wantOwns: true,
			wantOwned: []string{"internal/x.go"},
		},
		{
			name:        "listed owner but not of this path",
			login:       "alice",
			changed:     []string{"docs/api.md"}, // last match -> @carol
			wantPresent: true, wantIsOwner: true, wantOwns: false,
			wantOwned: nil,
		},
		{
			name:        "ext glob ownership, partial across changed set",
			login:       "bob",
			changed:     []string{"README.md", "docs/api.md"}, // bob owns README.md (*.md), carol owns docs/api.md
			wantPresent: true, wantIsOwner: true, wantOwns: true,
			wantOwned: []string{"README.md"},
		},
		{
			name:        "not an owner at all",
			login:       "eve",
			changed:     []string{"internal/x.go"},
			wantPresent: true, wantIsOwner: false, wantOwns: false,
			wantOwned: nil,
		},
		{
			name:        "login match is case-insensitive",
			login:       "ALICE",
			changed:     []string{"internal/sub/deep.go"}, // under /internal/
			wantPresent: true, wantIsOwner: true, wantOwns: true,
			wantOwned: []string{"internal/sub/deep.go"},
		},
		{
			name:        "catch-all owner owns an otherwise-unmatched path",
			login:       "maintainers",
			changed:     []string{"cmd/main.go"}, // only * matches
			wantPresent: true, wantIsOwner: true, wantOwns: true,
			wantOwned: []string{"cmd/main.go"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ParseCodeowners([]byte(sampleCodeowners), tc.login, tc.changed)
			if got.Present != tc.wantPresent {
				t.Errorf("Present = %v, want %v", got.Present, tc.wantPresent)
			}
			if got.IsCodeowner != tc.wantIsOwner {
				t.Errorf("IsCodeowner = %v, want %v", got.IsCodeowner, tc.wantIsOwner)
			}
			if got.OwnsChangedPaths != tc.wantOwns {
				t.Errorf("OwnsChangedPaths = %v, want %v", got.OwnsChangedPaths, tc.wantOwns)
			}
			if !slices.Equal(got.OwnedPaths, tc.wantOwned) {
				t.Errorf("OwnedPaths = %v, want %v", got.OwnedPaths, tc.wantOwned)
			}
		})
	}
}

func TestParseCodeowners_Empty(t *testing.T) {
	t.Parallel()
	got := ParseCodeowners(nil, "alice", []string{"x.go"})
	if got.Present || got.IsCodeowner || got.OwnsChangedPaths {
		t.Errorf("empty CODEOWNERS must yield a zero result, got %+v", got)
	}
}

// TestParseCodeowners_LongLineDoesNotDropRules guards against silent
// truncation: CODEOWNERS is attacker-editable, and a single
// pathologically long line must not cause the rules that follow it to be
// dropped. (bufio.Scanner's default 64 KiB token cap would stop the scan
// and swallow the error, flipping ownership signals to false.)
func TestParseCodeowners_LongLineDoesNotDropRules(t *testing.T) {
	t.Parallel()
	// A ~128 KiB comment line, well over bufio.Scanner's 64 KiB cap,
	// followed by a real ownership rule.
	long := "# " + strings.Repeat("x", 128*1024)
	content := []byte(long + "\n/secret/ @alice\n")

	got := ParseCodeowners(content, "alice", []string{"secret/data.txt"})
	if !got.Present {
		t.Fatal("rules after a long line were dropped (Present=false)")
	}
	if !got.IsCodeowner || !got.OwnsChangedPaths {
		t.Errorf("owner of the rule following a long line not detected: %+v", got)
	}
}

// TestParseCodeowners_MalformedAndSparseLines pins the parser's handling
// of the untrusted-input edge cases the code reasons about explicitly:
// interleaved comments/blank lines are skipped, a pattern with no owners
// "unsets" rather than attributing the bare pattern, and a malformed glob
// neither panics nor matches.
func TestParseCodeowners_MalformedAndSparseLines(t *testing.T) {
	t.Parallel()
	const content = `# leading comment

   # indented comment
*           @maintainers

/orphan/                     # pattern with no owners -> unsets, not a rule
src/[a-     @alice           # malformed glob: must not panic, must not match
docs/api.md @carol
`
	// Catch-all still attributes an otherwise-unmatched path.
	got := ParseCodeowners([]byte(content), "maintainers", []string{"cmd/main.go"})
	if !got.Present || !got.OwnsChangedPaths {
		t.Errorf("catch-all ownership lost amid comments/blank/sparse lines: %+v", got)
	}
	// The no-owner "/orphan/" line must not make anyone an owner of it; with
	// only the catch-all matching, @maintainers owns it, not a bare pattern.
	orphan := ParseCodeowners([]byte(content), "alice", []string{"orphan/file.txt"})
	if orphan.OwnsChangedPaths {
		t.Errorf("no-owner pattern should not attribute ownership to @alice: %+v", orphan)
	}
	// The malformed glob must not match (path.Match rejects it) and must
	// not have panicked to get here.
	bad := ParseCodeowners([]byte(content), "alice", []string{"src/a.go"})
	if bad.OwnsChangedPaths {
		t.Errorf("malformed glob should not match src/a.go: %+v", bad)
	}
}
