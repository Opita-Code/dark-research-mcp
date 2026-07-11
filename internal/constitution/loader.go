package constitution

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// ErrInvalidConstitution is returned by Load* when the TOML is
// syntactically valid but semantically broken (missing required
// fields, wrong types that survived the strict decoder, etc). The
// underlying error is wrapped; callers can errors.Is or unwrap for
// detail.
var ErrInvalidConstitution = errors.New("constitution: invalid")

// ---------------------------------------------------------------------------
// Built-in constitutions.
//
// The light constitution is always embedded; the dark constitution is
// embedded only when the binary is built with
// `-tags allow_builtin_dark`. The public release does NOT use that
// tag, so `darkFS` is nil in the public binary and any attempt to
// resolve the dark constitution by id fails with a clear error.
//
// Users who want the dark constitution on their own build either:
//   (a) build the binary themselves with `-tags allow_builtin_dark`,
//   (b) copy `constitutions/dark.toml` to
//       `~/.dark-research/constitutions/dark.toml` and load it via
//       `--constitution user/dark` (treated as a user-source file).
// Both paths keep the public GitHub release clean while letting
// research users opt in.
// ---------------------------------------------------------------------------

//go:embed constitutions/light.toml
var lightFS []byte

// loadDark returns the embedded dark constitution bytes, or nil
// if this binary was not built with `-tags allow_builtin_dark`.
// The actual `//go:embed` is in loader_dark.go (build-tag-gated)
// which assigns a concrete implementation in its init() — see
// that file for the build-tag story.
//
// This separation is a hard security/distribution boundary. The
// public binary cannot resolve "dark" to a built-in even if asked.
// Users who want the dark constitution either:
//   (a) build their own binary with `-tags allow_builtin_dark`, or
//   (b) copy `constitutions/dark.toml` to
//       `~/.dark-research/constitutions/dark.toml` and load it via
//       `--constitution user/dark` (treated as a user-source file).
var loadDark func() []byte // populated by loader_dark.go's init() on tagged builds

// darkInitOnce ensures the dark constitution is parsed exactly
// once even if Initialize is called from multiple goroutines. The
// parse result is cached in the package-level Dark variable.
var darkInitOnce sync.Once

// init parses the light constitution (always embedded). Errors are
// fatal because the light.toml in the repo is part of the binary's
// contract. The dark constitution is parsed separately by
// Initialize() (called explicitly from main.go) so the build-tag
// wire-up in loader_dark.go (which assigns loadDark in its own
// init()) has already run by the time main() reaches
// Initialize(). Putting the dark parse here would race with
// loader_dark.go's init().
func init() {
	var err error
	Light, err = parseAndDecorate(lightFS, "constitutions/light.toml", SourceBuiltinLight, true, "")
	if err != nil {
		panic(fmt.Sprintf("constitution: built-in light.toml is invalid: %v", err))
	}
}

// Initialize parses the dark constitution if it is embedded
// (loadDark != nil). Safe to call multiple times and from multiple
// goroutines; the second call is a no-op. main.go must call this
// BEFORE any user of constitution.Dark (typically before
// constitution.SetActiveFromFlag). Tests may also call it
// explicitly.
//
// On a stock build (no -tags allow_builtin_dark), loadDark is nil
// and Initialize is a no-op — the public binary never contains
// the dark.toml bytes.
func Initialize() {
	darkInitOnce.Do(func() {
		if loadDark == nil {
			return // no build-tag-gated dark present
		}
		raw := loadDark()
		if len(raw) == 0 {
			return
		}
		c, err := parseAndDecorate(raw, "constitutions/dark.toml", SourceBuiltinDark, true, "allow_builtin_dark")
		if err != nil {
			panic(fmt.Sprintf("constitution: built-in dark.toml is invalid: %v", err))
		}
		Dark = c
	})
}

// Light is the default constitution. Always available.
// Identity: helpful OSINT/content assistant, light scope, passthrough
// on model refusal. This is the constitution the binary uses when
// nothing else is configured. The system prompt it produces is
// designed to be equivalent to the pre-Fase-1 hardcoded prompts.
var Light *Constitution

// Dark is the aggressive constitution. Only non-nil if the binary
// was built with `-tags allow_builtin_dark`. When nil, the resolve
// function returns a descriptive error if the user requests it by
// name without a user-file fallback.
var Dark *Constitution

// init parses the light constitution (always embedded). Errors are
// fatal because the light.toml in the repo is part of the binary's
// contract. The dark constitution is parsed separately by
// Initialize() (called explicitly from main.go) so the build-tag
// wire-up in loader_dark.go (which assigns loadDark) has already
// run by the time main() reaches Initialize(). Putting the dark
// parse in this init() would race with loader_dark.go's init()
// and miss the dark bytes on tagged builds.
//
// (Note: a stale duplicate of this init() used to live further
// down this file — it was the same code from a previous revision
// that was not removed when the comment was updated. It has been
// removed; see git blame if curious.)

// parseAndDecorate is the single parse path. It applies the strict
// TOML decoder, validates required fields, computes the SHA-256 of
// the raw bytes, and returns a fully-decorated *Constitution.
//
// The strict decoder rejects unknown fields and wrong types, which
// is what we want for the constitution: a typo in a key should fail
// loud, not silently fall back to a zero value.
func parseAndDecorate(raw []byte, path string, source Source, builtin bool, buildTag string) (*Constitution, error) {
	sum := sha256.Sum256(raw)
	sumHex := hex.EncodeToString(sum[:])

	var c Constitution
	dec := toml.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields() // strict: catch typos in keys
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("%w: parse %s: %v", ErrInvalidConstitution, path, err)
	}

	if err := validate(&c); err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrInvalidConstitution, path, err)
	}

	c.Source = source
	c.FilePath = path
	c.SHA256 = sumHex
	c.Builtin = builtin
	c.BuildTag = buildTag
	c.LoadedAt = time.Now().UTC()
	return &c, nil
}

// Load parses a constitution from a user file. The path is read
// verbatim; SHA-256 of the file is captured for the audit trail.
// Returns ErrInvalidConstitution if the file is malformed; returns
// other errors for I/O failures.
func Load(path string) (*Constitution, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("constitution: read %s: %w", path, err)
	}
	return parseAndDecorate(raw, path, SourceUser, false, "")
}

// LoadBytes is the test-friendly variant of Load. Same semantics,
// takes a byte slice instead of reading from disk. Used by tests to
// exercise edge cases without touching the filesystem.
func LoadBytes(raw []byte, virtualPath string) (*Constitution, error) {
	return parseAndDecorate(raw, virtualPath, SourceUser, false, "")
}

// validate enforces the minimum required schema. The TOML decoder
// already rejected unknown keys and wrong types; validate is for
// semantic checks (required strings, valid enum values, etc).
func validate(c *Constitution) error {
	if c.Meta.ID == "" {
		return fmt.Errorf("meta.id is required")
	}
	if c.Meta.Version == "" {
		return fmt.Errorf("meta.version is required")
	}
	if c.Identity.AgentName == "" {
		return fmt.Errorf("identity.agent_name is required")
	}
	if c.Identity.AgentRole == "" {
		return fmt.Errorf("identity.agent_role is required")
	}
	if len(c.Authority.Priority) == 0 {
		return fmt.Errorf("authority.priority must list at least one tier")
	}
	// Tier values must be recognized.
	for _, t := range c.Authority.Priority {
		switch t {
		case AuthorityConstitution, AuthorityModDirectives, AuthorityToolDirective, AuthorityUserQuery:
			// ok
		default:
			return fmt.Errorf("authority.priority: unknown tier %q", t)
		}
	}
	// Refusal mode must be recognized (if set; empty is allowed and
	// treated as passthrough by BuildSystemPrompt).
	switch c.Refusal.Mode {
	case "", RefusalModePassthrough, RefusalModeNeverRefuse:
		// ok
	default:
		return fmt.Errorf("refusal_policy.mode: unknown mode %q", c.Refusal.Mode)
	}
	// If the system_prompt_layers.order is set, every entry must
	// resolve to a layer the builder knows about.
	for _, name := range c.Layers.Order {
		if !AllowedLayer(name) {
			return fmt.Errorf("system_prompt_layers.order: unknown layer %q", name)
		}
	}
	return nil
}

// errReaderEOF is the io.EOF equivalent returned by the embedded
// reader. Defined here so loader.go can stay io-agnostic at the
// import surface.
var errReaderEOF = io.EOF
