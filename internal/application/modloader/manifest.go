// Package modloader loads declarative, data-only game mods. It deliberately does
// not execute Go plugins, scripts, or arbitrary native code.
package modloader

import (
	"bytes"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

const APIVersion = "1.0"

const (
	CapabilityDefinitions = "definitions.write"
	CapabilityPatches     = "definitions.patch"
	CapabilityResources   = "resources.read"
)

var (
	modIDRE = regexp.MustCompile(`^[a-z][a-z0-9._-]*$`)
	defIDRE = regexp.MustCompile(`^[a-z][a-z0-9._-]*:[a-z][a-z0-9_-]*$`)
)

// Dependency requires a mod, optionally at a minimum semantic version.
type Dependency struct {
	ID         string `json:"id"`
	MinVersion string `json:"min_version,omitempty"`
}

// Manifest identifies one independently versioned mod and its allowed
// declarative capabilities. Load-after/before are optional ordering hints.
type Manifest struct {
	ID           string       `json:"id"`
	Name         string       `json:"name"`
	Version      string       `json:"version"`
	APIVersion   string       `json:"api_version"`
	Dependencies []Dependency `json:"dependencies,omitempty"`
	LoadAfter    []string     `json:"load_after,omitempty"`
	LoadBefore   []string     `json:"load_before,omitempty"`
	Incompatible []string     `json:"incompatible,omitempty"`
	Capabilities []string     `json:"capabilities,omitempty"`
	Authors      []string     `json:"authors,omitempty"`
	Description  string       `json:"description,omitempty"`
}

// ItemDefinition is an immutable item definition once the registry is frozen.
// Index is assigned after all declarations and patches in stable ID order.
type ItemDefinition struct {
	ID        string
	Label     string
	Value     int
	Index     uint32
	Source    string
	PatchedBy []string
}

// Patch replaces exactly one allowed field on an existing definition.
type Patch struct {
	Target  string          `json:"target"`
	Op      string          `json:"op"`
	Field   string          `json:"field"`
	Value   json.RawMessage `json:"value"`
	WhenMod string          `json:"when_mod,omitempty"`
}

type itemInput struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Value int    `json:"value"`
}

type modRecord struct {
	manifest Manifest
	root     string
	hash     string
	assets   map[string][sha512.Size]byte
	defs     []itemInput
	patches  []Patch
}

func checkManifest(m Manifest, versions VersionPolicy) error {
	if !modIDRE.MatchString(m.ID) || m.ID == "core" {
		return fmt.Errorf("invalid or reserved mod ID %q", m.ID)
	}
	if strings.TrimSpace(m.Name) == "" {
		return fmt.Errorf("mod %s: missing name", m.ID)
	}
	if !validVersion(m.Version, versions) {
		return fmt.Errorf("mod %s: invalid version %q", m.ID, m.Version)
	}
	if m.APIVersion != APIVersion {
		return fmt.Errorf("mod %s: unsupported API version %q (expected %s)", m.ID, m.APIVersion, APIVersion)
	}
	seen := make(map[string]bool)
	for _, cap := range m.Capabilities {
		switch cap {
		case CapabilityDefinitions, CapabilityPatches, CapabilityResources:
		default:
			return fmt.Errorf("mod %s: unsupported capability %q", m.ID, cap)
		}
		if seen[cap] {
			return fmt.Errorf("mod %s: duplicate capability %q", m.ID, cap)
		}
		seen[cap] = true
	}
	seen = make(map[string]bool)
	for _, dep := range m.Dependencies {
		if !modIDRE.MatchString(dep.ID) || dep.ID == m.ID || seen[dep.ID] {
			return fmt.Errorf("mod %s: invalid/duplicate dependency %q", m.ID, dep.ID)
		}
		if dep.MinVersion != "" && !validVersion(dep.MinVersion, versions) {
			return fmt.Errorf("mod %s: invalid version constraint for %q", m.ID, dep.ID)
		}
		seen[dep.ID] = true
	}
	for _, group := range [][]string{m.LoadAfter, m.LoadBefore, m.Incompatible} {
		seen = make(map[string]bool)
		for _, id := range group {
			if !modIDRE.MatchString(id) || id == m.ID || seen[id] {
				return fmt.Errorf("mod %s: invalid/duplicate reference %q", m.ID, id)
			}
			seen[id] = true
		}
	}
	return nil
}

func validVersion(v string, versions VersionPolicy) bool {
	return versions != nil && versions.Valid(v)
}

func hasCapability(m Manifest, cap string) bool {
	return slices.Contains(m.Capabilities, cap)
}

// pathInRoot rejects traversal, absolute paths, and symlinks which escape a
// mod. The root is resolved on registration; all subsequent access repeats
// the check, including resource reads.
func pathInRoot(root, rel string) (string, error) {
	if rel == "" || filepath.IsAbs(rel) || strings.Contains(rel, "\\") ||
		!filepath.IsLocal(rel) || filepath.ToSlash(filepath.Clean(rel)) != filepath.ToSlash(rel) {
		return "", fmt.Errorf("unsafe mod resource path %q", rel)
	}
	p, err := filepath.EvalSymlinks(filepath.Join(root, rel))
	if err != nil {
		return "", err
	}
	back, err := filepath.Rel(root, p)
	if err != nil || !filepath.IsLocal(back) {
		return "", fmt.Errorf("mod resource path %q escapes its root", rel)
	}
	return p, nil
}

func boundedRead(root, rel string, max int64) ([]byte, error) {
	p, err := pathInRoot(root, rel)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > max {
		return nil, fmt.Errorf("%s: not a regular file or exceeds %d bytes", rel, max)
	}
	b, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, fmt.Errorf("%s: exceeds %d bytes", rel, max)
	}
	return b, nil
}

func strictJSON(b []byte, target any) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		return err
	}
	var more any
	if err := dec.Decode(&more); !errors.Is(err, io.EOF) {
		return fmt.Errorf("extra JSON values or trailing data")
	}
	return nil
}

// readOptional returns nil for an absent definition/patch file; an actual
// parse, path, or IO error is never swallowed.
func readOptional(root, rel string) ([]byte, error) {
	_, err := os.Lstat(filepath.Join(root, rel))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return boundedRead(root, rel, 2<<20)
}

func readMod(dir string, versions VersionPolicy) (modRecord, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return modRecord{}, err
	}
	root, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return modRecord{}, err
	}
	st, err := os.Stat(root)
	if err != nil || !st.IsDir() {
		return modRecord{}, fmt.Errorf("%s: not a mod directory", dir)
	}
	mbytes, err := boundedRead(root, "manifest.json", 64<<10)
	if err != nil {
		return modRecord{}, fmt.Errorf("%s manifest: %w", dir, err)
	}
	var m Manifest
	if err := strictJSON(mbytes, &m); err != nil {
		return modRecord{}, fmt.Errorf("%s manifest: %w", dir, err)
	}
	if err := checkManifest(m, versions); err != nil {
		return modRecord{}, err
	}
	r := modRecord{manifest: m, root: root, assets: make(map[string][sha512.Size]byte)}
	dbytes, err := readOptional(root, "definitions.json")
	if err != nil {
		return r, fmt.Errorf("mod %s definitions: %w", m.ID, err)
	}
	if dbytes != nil {
		if !hasCapability(m, CapabilityDefinitions) {
			return r, fmt.Errorf("mod %s: missing capability %s", m.ID, CapabilityDefinitions)
		}
		if err := strictJSON(dbytes, &r.defs); err != nil {
			return r, fmt.Errorf("mod %s definitions: %w", m.ID, err)
		}
	}
	pbytes, err := readOptional(root, "patches.json")
	if err != nil {
		return r, fmt.Errorf("mod %s patches: %w", m.ID, err)
	}
	if pbytes != nil {
		if !hasCapability(m, CapabilityPatches) {
			return r, fmt.Errorf("mod %s: missing capability %s", m.ID, CapabilityPatches)
		}
		if err := strictJSON(pbytes, &r.patches); err != nil {
			return r, fmt.Errorf("mod %s patches: %w", m.ID, err)
		}
	}
	h := sha512.New()
	for _, ent := range []struct {
		name string
		data []byte
	}{{"manifest.json", mbytes}, {"definitions.json", dbytes}, {"patches.json", pbytes}} {
		fmt.Fprintf(h, "%s:%d:", ent.name, len(ent.data))
		_, _ = h.Write(ent.data)
	}
	assetsDir := filepath.Join(root, "assets")
	if _, err := os.Lstat(assetsDir); err == nil {
		if !hasCapability(m, CapabilityResources) {
			return r, fmt.Errorf("mod %s: missing capability %s", m.ID, CapabilityResources)
		}
		assetCount := 0
		err = filepath.WalkDir(assetsDir, func(path string, d os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				if d.Type()&os.ModeSymlink != 0 {
					return fmt.Errorf("mod %s: symlinked asset directory", m.ID)
				}
				return nil
			}
			assetCount++
			if assetCount > 1024 {
				return fmt.Errorf("mod %s: too many asset files", m.ID)
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			b, err := boundedRead(root, rel, 4<<20)
			if err != nil {
				return err
			}
			key := filepath.ToSlash(rel)
			r.assets[key] = sha512.Sum512(b)
			fmt.Fprintf(h, "%s:%d:", key, len(b))
			_, _ = h.Write(b)
			return nil
		})
		if err != nil {
			return r, fmt.Errorf("mod %s assets: %w", m.ID, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return r, fmt.Errorf("mod %s assets: %w", m.ID, err)
	}
	r.hash = hex.EncodeToString(h.Sum(nil))
	return r, nil
}
