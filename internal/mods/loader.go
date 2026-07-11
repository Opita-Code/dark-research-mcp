package mods

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// ErrNoManifest is returned when a mod directory has no mod.toml.
var ErrNoManifest = errors.New("mods: no mod.toml in directory")

// Load reads a mod from a directory. The path is the mod root
// (which must contain `mod.toml`). The returned `*Loaded` carries
// the parsed manifest plus the content of every referenced
// knowledge/directive file.
//
// Errors:
//   - ErrNoManifest: directory has no mod.toml
//   - ErrInvalidManifest: mod.toml exists but is malformed
//   - other errors: I/O failures (file not readable, etc)
func Load(modRoot string) (*Loaded, error) {
	manifestPath := filepath.Join(modRoot, "mod.toml")
	rawManifest, err := os.ReadFile(manifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrNoManifest, modRoot)
		}
		return nil, fmt.Errorf("mods: read %s: %w", manifestPath, err)
	}

	m, err := parseManifest(rawManifest, manifestPath)
	if err != nil {
		return nil, err
	}

	manifestSHA := sha256Hex(rawManifest)

	// Read every knowledge file.
	var knowledge []KnowledgeItem
	for _, rel := range m.Knowledge.PromptInjections {
		body, err := readModFile(modRoot, rel)
		if err != nil {
			return nil, fmt.Errorf("mods: read knowledge %s: %w", rel, err)
		}
		knowledge = append(knowledge, KnowledgeItem{
			Path:   rel,
			Body:   body,
			Kind:   "prompt_injection",
			SHA256: sha256Hex([]byte(body)),
		})
	}
	for _, rel := range m.Knowledge.DataSources {
		body, err := readModFile(modRoot, rel)
		if err != nil {
			return nil, fmt.Errorf("mods: read knowledge %s: %w", rel, err)
		}
		knowledge = append(knowledge, KnowledgeItem{
			Path:   rel,
			Body:   body,
			Kind:   "data_source",
			SHA256: sha256Hex([]byte(body)),
		})
	}

	// Read every directive file.
	var directives []DirectiveItem
	for _, rel := range m.Directives.PromptFragments {
		body, err := readModFile(modRoot, rel)
		if err != nil {
			return nil, fmt.Errorf("mods: read directive %s: %w", rel, err)
		}
		directives = append(directives, DirectiveItem{
			Path:   rel,
			Body:   body,
			SHA256: sha256Hex([]byte(body)),
		})
	}

	return &Loaded{
		Manifest:   *m,
		Path:       modRoot,
		SHA256:     manifestSHA,
		LoadedAt:   time.Now().UTC(),
		Source:     SourceUser, // future: registry installation sets this to SourceRegistry
		Knowledge:  knowledge,
		Directives: directives,
	}, nil
}

// LoadManifestBytes is the test-friendly variant of Load. Takes
// the raw manifest bytes and a virtual path (used in error
// messages and as the Loaded.Path value). Does NOT read any
// knowledge/directive files — for tests that want to exercise
// manifest parsing without touching the filesystem.
//
// To exercise file loading, use Load with a temp directory.
func LoadManifestBytes(rawManifest []byte, virtualPath string) (*Loaded, error) {
	m, err := parseManifest(rawManifest, virtualPath)
	if err != nil {
		return nil, err
	}
	return &Loaded{
		Manifest:   *m,
		Path:       virtualPath,
		SHA256:     sha256Hex(rawManifest),
		LoadedAt:   time.Now().UTC(),
		Source:     SourceUser,
		Knowledge:  nil,
		Directives: nil,
	}, nil
}

// parseManifest applies the strict decoder, validates required
// fields, and returns a parsed Manifest. The strict decoder rejects
// unknown keys so a typo in the manifest fails loud.
func parseManifest(raw []byte, sourcePath string) (*Manifest, error) {
	var m Manifest
	dec := toml.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("%w: parse %s: %v", ErrInvalidManifest, sourcePath, err)
	}
	if err := validateManifest(&m); err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrInvalidManifest, sourcePath, err)
	}
	return &m, nil
}

// validateManifest enforces the minimum required schema. The TOML
// decoder already rejected unknown keys; validate is for semantic
// checks (required strings, path safety, known enum values).
func validateManifest(m *Manifest) error {
	if m.Meta.ID == "" {
		return fmt.Errorf("meta.id is required")
	}
	if m.Meta.Version == "" {
		return fmt.Errorf("meta.version is required")
	}
	if m.Meta.Name == "" {
		return fmt.Errorf("meta.name is required")
	}
	// mod_id format check: must be "namespace/name". The slash is
	// what the registry (future) and the on-disk convention use to
	// separate the namespace from the mod's short name.
	if !strings.Contains(m.Meta.ID, "/") {
		return fmt.Errorf("meta.id must be 'namespace/name' (got %q)", m.Meta.ID)
	}
	// Risk class must be recognized if set.
	if m.Risk.Class != "" {
		switch m.Risk.Class {
		case string(RiskClassResearchOnly), string(RiskClassActiveProbing), string(RiskClassExploitDevelopment):
			// ok
		default:
			return fmt.Errorf("risk.risk_class: unknown value %q", m.Risk.Class)
		}
	}
	// Target scope must be recognized if set.
	if m.Risk.TargetScope != "" {
		switch m.Risk.TargetScope {
		case string(TargetScopePublicInternet), string(TargetScopePrivateInfrastructure),
			string(TargetScopeDarkweb), string(TargetScopeLocalOnly):
			// ok
		default:
			return fmt.Errorf("risk.target_scope: unknown value %q", m.Risk.TargetScope)
		}
	}
	// Path safety: every knowledge/directive path must be
	// relative (no leading slash, no ".." components). Prevents a
	// mod from reading /etc/passwd or similar.
	allPaths := append([]string{}, m.Knowledge.PromptInjections...)
	allPaths = append(allPaths, m.Knowledge.DataSources...)
	allPaths = append(allPaths, m.Directives.PromptFragments...)
	for _, p := range allPaths {
		if err := validateModPath(p); err != nil {
			return fmt.Errorf("invalid path %q: %w", p, err)
		}
	}
	// Capability paths are reserved for Fase 6 (Go plugins). We
	// just sanity-check the format.
	for _, p := range m.Capabilities.Tools {
		if err := validateModPath(p); err != nil {
			return fmt.Errorf("capabilities.tools: invalid path %q: %w", p, err)
		}
	}
	for _, p := range m.Capabilities.Parsers {
		if err := validateModPath(p); err != nil {
			return fmt.Errorf("capabilities.parsers: invalid path %q: %w", p, err)
		}
	}
	for _, p := range m.Capabilities.Backends {
		if err := validateModPath(p); err != nil {
			return fmt.Errorf("capabilities.backends: invalid path %q: %w", p, err)
		}
	}
	return nil
}

// validateModPath checks a path is relative and does not escape
// the mod root. Absolute paths and ".." components are rejected.
func validateModPath(p string) error {
	if p == "" {
		return fmt.Errorf("empty path")
	}
	if filepath.IsAbs(p) {
		return fmt.Errorf("absolute paths not allowed")
	}
	// Clean and check for ".." leakage.
	cleaned := filepath.Clean(p)
	if strings.HasPrefix(cleaned, "..") || strings.Contains(cleaned, "/../") || strings.HasSuffix(cleaned, "/..") {
		return fmt.Errorf("path escapes mod root")
	}
	return nil
}

// readModFile reads a file from inside the mod root, enforcing
// the path safety rules. The relative path must not escape the
// mod root.
func readModFile(modRoot, rel string) (string, error) {
	if err := validateModPath(rel); err != nil {
		return "", err
	}
	full := filepath.Join(modRoot, rel)
	// Defense in depth: verify the resolved path is still inside
	// the mod root (in case filepath.Clean produced a surprise).
	absRoot, err := filepath.Abs(modRoot)
	if err != nil {
		return "", fmt.Errorf("resolve mod root: %w", err)
	}
	absFull, err := filepath.Abs(full)
	if err != nil {
		return "", fmt.Errorf("resolve file: %w", err)
	}
	if !strings.HasPrefix(absFull, absRoot+string(filepath.Separator)) && absFull != absRoot {
		return "", fmt.Errorf("path escapes mod root after resolution: %s", rel)
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// sha256Hex returns the lowercase hex SHA-256 of b.
func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
