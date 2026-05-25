// Package credentials maps source-control vendors to their token
// environment variables. pr-analyzer never stores, transmits, or
// caches tokens — this package only describes which env var to read
// and reports presence, so the CLI can surface a clear error when a
// list-mode (or other multi-call) operation would otherwise hit
// GitHub anonymously and be rate-limited.
//
// The Vendor type is generic over source-control hosts. Today only
// GitHub is seeded; adding GitLab, Bitbucket, or a corporate Gerrit
// is one new package-level variable.
package credentials

import (
	"fmt"
	"os"
)

// Vendor identifies a source-control host by its display name and the
// OS environment variable that carries its API token.
type Vendor struct {
	// Name is the human-readable vendor label that appears in error
	// messages, e.g. "GitHub".
	Name string
	// EnvVar is the name of the environment variable that holds the
	// vendor's API token, e.g. "GITHUB_TOKEN".
	EnvVar string
}

// GitHub is the built-in vendor for github.com.
var GitHub = Vendor{Name: "GitHub", EnvVar: "GITHUB_TOKEN"}

// Token returns the current value of the vendor's env var.
// Empty string means "unset or set to empty"; callers treat both the
// same.
func (v Vendor) Token() string {
	return os.Getenv(v.EnvVar)
}

// Require returns nil when Token() is non-empty, otherwise an error
// naming both the vendor and the env var the user must set. Callers
// use this at the boundary of operations that would issue many API
// calls — for those, an unauthenticated rate limit is too low to be
// useful and a clear up-front error beats a mid-scan 403.
func (v Vendor) Require() error {
	if v.Token() == "" {
		return fmt.Errorf("%s authentication required: set %s in the environment", v.Name, v.EnvVar)
	}
	return nil
}
