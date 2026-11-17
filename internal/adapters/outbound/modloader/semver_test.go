package modloader

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appmodloader "rimworld-go/internal/application/modloader"
)

func TestSemanticVersions(t *testing.T) {
	policy := SemanticVersions{}
	for _, version := range []string{"1.2.3", "1.2.3-rc.1", "1.2.3+build.4"} {
		if !policy.Valid(version) {
			t.Errorf("Valid(%q) = false", version)
		}
	}
	for _, version := range []string{"", "v1.2.3", "1.2", "broken"} {
		if policy.Valid(version) {
			t.Errorf("Valid(%q) = true", version)
		}
	}
	if got := policy.Compare("1.10.0", "1.9.9"); got <= 0 {
		t.Fatalf("Compare(1.10.0, 1.9.9) = %d", got)
	}
	if got := policy.Compare("2.0.0-rc.1", "2.0.0"); got >= 0 {
		t.Fatalf("Compare(2.0.0-rc.1, 2.0.0) = %d", got)
	}
}

func TestMinimumDependencyUsesSemanticVersionPolicy(t *testing.T) {
	root := t.TempDir()
	writeManifest := func(id, version string, dependencies []appmodloader.Dependency) string {
		t.Helper()
		dir := filepath.Join(root, id)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		manifest := appmodloader.Manifest{
			ID: id, Name: id, Version: version, APIVersion: appmodloader.APIVersion,
			Dependencies: dependencies,
		}
		data, err := json.Marshal(manifest)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0o600); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	base := writeManifest("example.base", "1.9.9", nil)
	dependent := writeManifest("example.dependent", "1.0.0", []appmodloader.Dependency{
		{ID: "example.base", MinVersion: "1.10.0"},
	})
	if _, err := appmodloader.Load(SemanticVersions{}, base, dependent); err == nil || !strings.Contains(err.Error(), "needs >= 1.10.0") {
		t.Fatalf("minimum version accepted older dependency: %v", err)
	}
	base = writeManifest("example.base", "1.10.0", nil)
	if _, err := appmodloader.Load(SemanticVersions{}, base, dependent); err != nil {
		t.Fatalf("minimum version rejected compatible dependency: %v", err)
	}
}
