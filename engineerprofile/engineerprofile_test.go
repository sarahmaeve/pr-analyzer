package engineerprofile_test

import (
	"testing"

	"github.com/sarahmaeve/pr-analyzer/engineerprofile"
)

func TestCollect_AuthorAssociation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   engineerprofile.Input
		want string
	}{
		{"empty", engineerprofile.Input{}, ""},
		{"FIRST_TIME_CONTRIBUTOR is preserved verbatim", engineerprofile.Input{AuthorAssociation: "FIRST_TIME_CONTRIBUTOR"}, "FIRST_TIME_CONTRIBUTOR"},
		{"OWNER is preserved verbatim (no filtering at collection layer)", engineerprofile.Input{AuthorAssociation: "OWNER"}, "OWNER"},
		{"unknown future value is preserved verbatim", engineerprofile.Input{AuthorAssociation: "GITHUB_FUTURE_VALUE"}, "GITHUB_FUTURE_VALUE"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := engineerprofile.Collect(tc.in).AuthorAssociation
			if got != tc.want {
				t.Errorf("Collect(%+v).AuthorAssociation = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
