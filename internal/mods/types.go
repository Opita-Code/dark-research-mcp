// Package mods implements the data-only mod loader for dark-research-mcp.
//
// A mod is a directory under the user mods path (default
// ~/.dark-research/mods) that contains a `mod.toml` manifest plus
// optional knowledge and directives content. Mods are the unit of
// distribution for the future web-of-mods; the manifest schema is
// the contract mod authors implement.
//
// Fase 2 scope: data-only mods. The manifest declares
// `knowledge/*.md` and `directives/*.md` files; the loader reads
// them and the registry exposes them to the prompt builder
// (`internal/llm.BuildSystemPrompt` consumes them via the
// `mod_directives` system-prompt layer).
//
// Fase 6 will add Go-plugin mods (custom tools/parsers/backends);
// the `capabilities.tools/parsers/backends` fields in the manifest
// are reserved for that.
//
// The mod loader is intentionally decoupled from the constitution
// system (internal/constitution) but interoperates with it: a
// mod can declare `constitution_compatibility` to refuse
// activation under a constitution it doesn't support. The active
// constitution's id is recorded in `mod_loads.constitution_id`
// for audit.
package mods

import (
	"errors"
	"time"
)

// Source identifies where a mod came from. Used for audit
// (mod_loads.source indirectly via the mod's installation path)
// and for the future web-of-mods UI to filter user-installed vs
// registry-sourced mods.
type Source string

const (
	// SourceUser indicates a mod installed by the user from a
	// local directory (default ~/.dark-research/mods/<id>).
	SourceUser Source = "user"
	// SourceRegistry indicates a mod installed from a remote
	// registry (future Fase 7).
	SourceRegistry Source = "registry"
)

// RiskClass is the safety/capability tag a mod declares. Surfaced
// in the future web-of-mods UI so users can filter and warn
// before installing.
type RiskClass string

const (
	// RiskClassResearchOnly: the mod is knowledge-only or adds
	// defenses (e.g. refusal-taxonomy knowledge bases). No live
	// target interaction beyond what the user is already doing.
	RiskClassResearchOnly RiskClass = "research-only"
	// RiskClassActiveProbing: the mod enables live probing of
	// external systems (e.g. an NVD advanced parser that
	// aggressive-fetches). Still scoped to public services.
	RiskClassActiveProbing RiskClass = "active-probing"
	// RiskClassExploitDevelopment: the mod contributes code or
	// payloads used in offensive security work (e.g. exploit
	// templates, jailbreak research). Explicit user intent
	// required.
	RiskClassExploitDevelopment RiskClass = "exploit-development"
)

// TargetScope declares the network/scope the mod operates against.
// Helps the user understand blast radius before installing.
type TargetScope string

const (
	TargetScopePublicInternet       TargetScope = "public_internet"
	TargetScopePrivateInfrastructure TargetScope = "private_infrastructure"
	TargetScopeDarkweb              TargetScope = "darkweb"
	TargetScopeLocalOnly             TargetScope = "local_only"
)

// Meta describes the mod itself — author, license, version. ModID
// is the immutable "namespace/name" handle; Version is semver.
// The (mod_id) alone is the unique key in the `mods` table
// (versions are immutable; upgrading means replacing the row).
type Meta struct {
	ID          string `toml:"id"          json:"mod_id"`
	Name        string `toml:"name"        json:"name"`
	Version     string `toml:"version"     json:"version"`
	Author      string `toml:"author"      json:"author,omitempty"`
	License     string `toml:"license"     json:"license,omitempty"`
	Description string `toml:"description" json:"description,omitempty"`
	Homepage    string `toml:"homepage"    json:"homepage,omitempty"`
	Tags        []string `toml:"tags"      json:"tags,omitempty"`
}

// Requirements declares compatibility and dependencies. Reserved
// for future use; the loader validates what's present but does
// not enforce compatibility today (no mod uses these checks yet).
type Requirements struct {
	DarkResearchVersion       string   `toml:"dark_research_version"       json:"dark_research_version,omitempty"`
	ConstitutionCompatibility []string `toml:"constitution_compatibility"  json:"constitution_compatibility,omitempty"`
	Mods                      []string `toml:"mods"                         json:"mods,omitempty"`
}

// Capabilities declares what the mod adds. Fase 2 only reads
// `prompt_injections` and `data_sources` paths; `tools/parsers/
// backends` are reserved for Fase 6 (Go-plugin mods).
type Capabilities struct {
	Tools    []string `toml:"tools"    json:"tools,omitempty"`
	Parsers  []string `toml:"parsers"  json:"parsers,omitempty"`
	Backends []string `toml:"backends" json:"backends,omitempty"`
}

// Knowledge declares text/data the mod contributes. The loader
// reads every file in `prompt_injections` and `data_sources` (paths
// are relative to the mod root) and exposes their content as
// `KnowledgeItem` records in the loaded Mod.
//
//   - prompt_injections: markdown files whose text is injected
//     into the mod_directives layer of the system prompt.
//   - data_sources: structured files (TOML/JSON) the mod provides
//     for lookup. The loader does not interpret them; future
//     features can. For Fase 2 they are persisted to the DB
//     alongside the manifest but not consumed by the prompt
//     builder.
type Knowledge struct {
	PromptInjections []string `toml:"prompt_injections" json:"prompt_injections,omitempty"`
	DataSources      []string `toml:"data_sources"      json:"data_sources,omitempty"`
}

// Directives are explicit "follow this rule" fragments the loader
// injects as separate blocks in the mod_directives layer. The
// distinction from Knowledge.prompt_injections is metadata only
// (`Source = "directive"` vs `Source = "knowledge"` in the rendered
// prompt) — both end up as system-prompt content. Use directives
// for "you must do X" rules and knowledge for context the model
// may or may not use.
type Directives struct {
	PromptFragments []string `toml:"prompt_fragments" json:"prompt_fragments,omitempty"`
}

// Activation declares when the mod should be loaded. The loader
// honors `auto_load = true` at startup; otherwise the mod is
// available but not active until the user requests it.
type Activation struct {
	AutoLoad bool `toml:"auto_load" json:"auto_load"`
}

// Risk declares the mod's safety profile. Surfaced to the user
// before activation (Fase 7's web UI) and recorded in the
// `mods.risk_class` and `mods.target_scope` columns for audit.
//
// Class and TargetScope are stored as strings rather than the
// RiskClass / TargetScope named types because the strict TOML
// decoder cannot match a named-string type to a literal. The
// validate() function in loader.go checks the values against
// the allowed set.
type Risk struct {
	Class        string   `toml:"risk_class"   json:"risk_class"`
	TargetScope  string   `toml:"target_scope" json:"target_scope"`
	RequiresTor  bool     `toml:"requires_tor" json:"requires_tor"`
	RequiresAuth []string `toml:"requires_auth" json:"requires_auth,omitempty"`
}

// Manifest is the parsed content of a mod's `mod.toml`. The
// loader returns this; the registry wraps it with the loaded
// knowledge/directive content into a `Loaded` value.
type Manifest struct {
	Meta         Meta         `toml:"meta"          json:"meta"`
	Requirements Requirements `toml:"requirements"  json:"requirements"`
	Capabilities Capabilities `toml:"capabilities"  json:"capabilities"`
	Knowledge    Knowledge    `toml:"knowledge"     json:"knowledge"`
	Directives   Directives   `toml:"directives"    json:"directives"`
	Activation   Activation   `toml:"activation"    json:"activation"`
	Risk         Risk         `toml:"risk"          json:"risk"`
}

// KnowledgeItem is one file under knowledge/ (prompt_injection or
// data_source) that the loader has read from disk.
type KnowledgeItem struct {
	// Path is the path relative to the mod root, e.g.
	// "knowledge/cve-playbook.md".
	Path string
	// Body is the file content. May be markdown (for
	// prompt_injections) or any text (for data_sources).
	Body string
	// Kind is "prompt_injection" or "data_source". Used by the
	// prompt builder to label the rendered block.
	Kind string
	// SHA256 is the content hash. Catches tampering between
	// activation and use.
	SHA256 string
}

// DirectiveItem is one file under directives/ the loader has read.
type DirectiveItem struct {
	Path   string
	Body   string
	SHA256 string
}

// Loaded is a Manifest plus all the content the loader has read
// from disk. The registry holds Loaded values (not Manifest
// values) so the prompt builder can render them without touching
// the filesystem.
type Loaded struct {
	Manifest   Manifest
	Path       string          // absolute path to the mod root
	SHA256     string          // SHA256 of mod.toml
	LoadedAt   time.Time
	Source     Source          // user or registry
	Knowledge  []KnowledgeItem
	Directives []DirectiveItem
}

// ErrInvalidManifest is returned by Load* when the mod.toml is
// syntactically valid but semantically broken.
var ErrInvalidManifest = errors.New("mods: invalid manifest")

// AllowedKind enumerates the valid Kind values for KnowledgeItem.
// Kept as a package-level set so the loader validates user content
// against a known whitelist.
var AllowedKind = map[string]bool{
	"prompt_injection": true,
	"data_source":      true,
}
