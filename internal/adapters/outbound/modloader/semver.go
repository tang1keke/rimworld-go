// Package modloader adapts the semantic-version library to the application's
// version-policy boundary for declarative mod manifests.
package modloader

import (
	"strings"

	"golang.org/x/mod/semver"

	appmodloader "rimworld-go/internal/application/modloader"
)

// SemanticVersions is the outbound implementation of the application's
// semantic-version policy. Manifest versions omit the conventional leading v.
type SemanticVersions struct{}

var _ appmodloader.VersionPolicy = SemanticVersions{}

// Valid accepts complete semantic versions without a leading v.
func (SemanticVersions) Valid(version string) bool {
	return !strings.HasPrefix(version, "v") && semver.IsValid("v"+version) && strings.Count(version, ".") >= 2
}

// Compare compares two manifest versions without their leading v.
func (SemanticVersions) Compare(left, right string) int {
	return semver.Compare("v"+left, "v"+right)
}
