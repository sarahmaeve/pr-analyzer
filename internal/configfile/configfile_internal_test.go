package configfile

import (
	"path/filepath"
	"testing"
)

// TestUserConfigPath pins the slice-6 user-level discovery rule:
// XDG_CONFIG_HOME takes precedence when set, otherwise we synthesize
// the XDG-default fallback under $HOME/.config. When neither is set
// the function returns "" so the caller knows there's no user-level
// config to check.
func TestUserConfigPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		xdg  string
		home string
		want string
	}{
		{
			name: "XDG_CONFIG_HOME set — XDG path wins",
			xdg:  "/tmp/x",
			home: "/tmp/h",
			want: filepath.Join("/tmp/x", "pr-analyzer", "pr-analyzer.yaml"),
		},
		{
			name: "XDG empty, HOME set — XDG-default fallback under .config",
			xdg:  "",
			home: "/tmp/h",
			want: filepath.Join("/tmp/h", ".config", "pr-analyzer", "pr-analyzer.yaml"),
		},
		{
			name: "neither set — no user-level path",
			xdg:  "",
			home: "",
			want: "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := userConfigPath(tc.xdg, tc.home)
			if got != tc.want {
				t.Errorf("userConfigPath(%q, %q) = %q, want %q", tc.xdg, tc.home, got, tc.want)
			}
		})
	}
}
