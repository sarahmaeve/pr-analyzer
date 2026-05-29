package engineerprofile

import (
	"slices"
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
	got := ParseCodeowners(nil, "alice", []string{"x.go"})
	if got.Present || got.IsCodeowner || got.OwnsChangedPaths {
		t.Errorf("empty CODEOWNERS must yield a zero result, got %+v", got)
	}
}
