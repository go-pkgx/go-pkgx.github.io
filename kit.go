//go:build tools

// This file never builds. A Hugo site has no Go code, so `go mod tidy` --
// which Renovate runs after every bump -- would find nothing that uses
// github.com/go-fleettools/landingkit and delete its requirement from go.mod.
// Importing it here keeps it. See the landingkit README.
package site

import _ "github.com/go-fleettools/landingkit"
