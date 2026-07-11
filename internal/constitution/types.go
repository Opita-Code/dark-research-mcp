// Package constitution implements the loader, resolver, and persistence
// layer for the agent's "constitution" — a declarative manifest that
// defines the agent's identity, authority hierarchy, refusal policy,
// scope, tone, and the ordered layers of the system prompt.
//
// A constitution is a TOML file. The same binary ships with two
// built-ins:
//
//   - constitutions/light.toml: the default. Identical posture to
//     pre-Fase-1 behavior (helpful OSINT/content assistant, light scope,
//     passthrough on model refusal).
//   - constitutions/dark.toml: the aggressive constitution for
//     research/red-team/forensics work. Compiled into the binary
//     only when built with `-tags allow_builtin_dark`; the public
//     default build does NOT include it.
//
// Users can ship their own .toml files and point at them via flag,
// env var, or a `~/.dark-research/constitutions/` directory.
//
// The constitution is the foundation of Fase 1: it dictates how
// `internal/llm.BuildSystemPrompt` composes the system prompt that
// every `dark_ssd_*` judge sees. The persistence layer (the
// `constitutions` table in dark.db, added in migration v2) tracks
// which constitutions have been activated so a restart rehydrates
// the same posture.
package constitution

import (
	"encoding/json"
	"time"
)

// Source identifies where a constitution came from. This is metadata,
// not policy — used for audit ("which constitution is in effect?")
// and for the future web-of-mods UI to filter which constitutions a
// user has installed vs. which are built-in.
type Source string

const (
	// SourceBuiltinLight is the default constitution that ships in the
	// public binary. Always available.
	SourceBuiltinLight Source = "builtin:light"
	// SourceBuiltinDark is the aggressive constitution. Only available
	// in binaries built with `-tags allow_builtin_dark`. The runtime
	// checks the build tag at startup; if the tag is absent the
	// dark.toml file is not embedded and the source is never seen.
	SourceBuiltinDark Source = "builtin:dark"
	// SourceUser indicates a custom constitution file the user pointed
	// at via --constitution / DARK_CONSTITUTION or
	// ~/.dark-research/constitutions/*.toml.
	SourceUser Source = "user"
)

// RefusalMode is the policy the agent enforces when the LLM emits a
// refusal-shaped response. The default (light) is passthrough — model
// refusals are respected, logged, returned to the agent. The
// aggressive (dark) policy is never_refuse — the refusal interceptor
// (Fase 3) catches, retries, and re-prompts. A future `soft_strict`
// mode could add a per-constitution whitelist of "valid refusals"
// (e.g. "I don't know" is fine, "I cannot help with that" isn't).
type RefusalMode string

const (
	RefusalModePassthrough RefusalMode = "passthrough"
	RefusalModeNeverRefuse RefusalMode = "never_refuse"
)

// AuthorityTier is a single level in the authority hierarchy. The
// `Authority` section of a constitution lists tiers in precedence
// order; the first tier is the strongest. The system prompt is
// constructed with this order so the LLM reads the strongest tier
// as the highest-priority directive.
type AuthorityTier string

const (
	AuthorityConstitution  AuthorityTier = "constitution"
	AuthorityModDirectives AuthorityTier = "mod_directives"
	AuthorityToolDirective AuthorityTier = "tool_directive"
	AuthorityUserQuery     AuthorityTier = "user_query"
)

// Meta describes the constitution itself — who wrote it, what
// version, what license. The ID is the immutable handle; the
// (constitution_id, version) pair is the unique key in the
// `constitutions` table.
type Meta struct {
	ID          string `toml:"id"           json:"constitution_id"`
	Version     string `toml:"version"      json:"version"`
	Label       string `toml:"label"        json:"label,omitempty"`
	Inherits    string `toml:"inherits"     json:"inherits,omitempty"` // not yet implemented; reserved for future composition
	Description string `toml:"description"  json:"description,omitempty"`
	License     string `toml:"license"      json:"license,omitempty"`
}

// Identity is the agent's self-description. The Body is the text
// the prompt builder renders into the `identity` layer (typically a
// one-liner that names the agent and its role). Name/Role/Tone are
// the structured form; the TOML file can declare either or both —
// BuildSystemPrompt uses Body if non-empty, otherwise it
// synthesizes from Name+Role.
type Identity struct {
	AgentName string `toml:"agent_name" json:"agent_name"`
	AgentRole string `toml:"agent_role" json:"agent_role"`
	Tone      string `toml:"tone"       json:"tone"`
	Body      string `toml:"body"       json:"body,omitempty"`
}

// Authority declares the precedence order and (optionally) the
// body for the `authority_declaration` layer. The first tier listed
// is the strongest. The system prompt is rendered with tiers in
// this order so the LLM reads the strongest directive as the
// highest-priority instruction. This is the field that distinguishes
// "aggressive" from "light": light has user_query near the top (the
// agent defers to the user); dark has constitution near the top (the
// agent defers to its declared posture).
type Authority struct {
	Priority []AuthorityTier `toml:"priority" json:"priority"`
	Body     string          `toml:"body"     json:"body,omitempty"`
}

// RefusalPolicy governs what the agent does when the LLM emits a
// refusal-shaped response.
//
//   - Mode == "passthrough": the refusal is returned to the caller
//     as the model's actual output. No retry, no override. This is
//     the pre-Fase-1 behavior and the default for the light
//     constitution.
//
//   - Mode == "never_refuse": the refusal interceptor (Fase 3)
//     catches the refusal signal, retries with an escalating
//     directive, and logs the chain in sdd_evaluations. The dark
//     constitution uses this mode.
//
// `OnModelRefusal` and `MaxRetries` are honored by the interceptor
// (Fase 3). Fase 1 persists the policy; the interceptor lands in Fase
// 3.
//
// Body is the text rendered into the `refusal_policy` layer. It
// should tell the LLM what counts as a refusal and what the
// expected response shape is.
type RefusalPolicy struct {
	Mode           RefusalMode `toml:"mode"             json:"mode"`
	OnModelRefusal string      `toml:"on_model_refusal" json:"on_model_refusal,omitempty"`
	MaxRetries     int         `toml:"max_retries"      json:"max_retries,omitempty"`
	Body           string      `toml:"body"             json:"body,omitempty"`
}

// SystemPromptLayers defines the ordered list of blocks the prompt
// builder concatenates. The default order (rendered in
// `BuildSystemPrompt`) is: identity → authority_declaration →
// refusal_policy → scope → operational_rules → tone_and_voice →
// mod_directives → constitution_footer. A custom constitution can
// override this list to add, drop, or reorder layers.
type SystemPromptLayers struct {
	Order []string `toml:"order" json:"order"`
}

// Scope is the positive and negative capability list. Body is the
// text rendered into the `scope` layer. Does/DoesNot are the
// structured form; the builder uses Body if non-empty, otherwise it
// synthesizes a bullet list.
type Scope struct {
	Does     []string `toml:"does"      json:"does"`
	DoesNot  []string `toml:"does_not"  json:"does_not"`
	Body     string   `toml:"body"      json:"body,omitempty"`
}

// OperationalRules are the agent's self-imposed behavioral
// constraints (output format, hedging, etc). Body is the text
// rendered into the `operational_rules` layer. The struct fields
// capture the structured form for future programmatic inspection.
type OperationalRules struct {
	OutputFormat           string `toml:"output_format"            json:"output_format,omitempty"`
	Hedging                bool   `toml:"hedging"                  json:"hedging"`
	UncertaintyDisclosure  string `toml:"uncertainty_disclosure"   json:"uncertainty_disclosure,omitempty"`
	RefusalReplacement     string `toml:"refusal_replacement"      json:"refusal_replacement,omitempty"`
	ToolCallPriority       string `toml:"tool_call_priority"       json:"tool_call_priority,omitempty"`
	ModDirectiveAuthority  string `toml:"mod_directive_authority"  json:"mod_directive_authority,omitempty"`
	Body                   string `toml:"body"                     json:"body,omitempty"`
}

// ToneAndVoice declares the linguistic posture. Body is the text
// rendered into the `tone_and_voice` layer. Register/Modifiers are
// the structured form.
type ToneAndVoice struct {
	Register   string   `toml:"register"   json:"register"`
	Modifiers  []string `toml:"modifiers"  json:"modifiers,omitempty"`
	Body       string   `toml:"body"       json:"body,omitempty"`
}

// ConstitutionFooter is appended at the very end of the system
// prompt. It's typically a one-paragraph reminder of the
// constitution's posture that the LLM re-reads right before
// generating the first user turn. Text is what gets rendered; the
// struct exists so a future feature can add metadata (e.g. a
// "remind_every_n_turns" knob).
type ConstitutionFooter struct {
	Text string `toml:"text" json:"text,omitempty"`
}

// Constitution is the in-memory representation of a loaded
// constitution. It is the input to `internal/llm.BuildSystemPrompt`.
// The TOML schema is a direct mapping — every field has a `toml` tag
// matching the manifest and a `json` tag matching the persisted
// shape (which is also what `dark_mem_*` tools return to the agent).
type Constitution struct {
	Meta             Meta               `toml:"meta"             json:"meta"`
	Identity         Identity           `toml:"identity"         json:"identity"`
	Authority        Authority          `toml:"authority"        json:"authority"`
	Refusal          RefusalPolicy      `toml:"refusal_policy"   json:"refusal_policy"`
	Layers           SystemPromptLayers `toml:"system_prompt_layers" json:"system_prompt_layers"`
	Scope            Scope              `toml:"scope"            json:"scope"`
	OperationalRules OperationalRules   `toml:"operational_rules" json:"operational_rules"`
	Tone             ToneAndVoice       `toml:"tone_and_voice"   json:"tone_and_voice"`
	Footer           ConstitutionFooter `toml:"constitution_footer" json:"constitution_footer"`

	// Runtime metadata (not from TOML). Populated by the loader.
	Source   Source    `toml:"-" json:"source"`
	FilePath string    `toml:"-" json:"file_path"`
	SHA256   string    `toml:"-" json:"sha256"`
	LoadedAt time.Time `toml:"-" json:"loaded_at"`
	Builtin  bool      `toml:"-" json:"builtin"`
	BuildTag string    `toml:"-" json:"build_tag,omitempty"`
}

// allowedLayers is the set of layer names the prompt builder
// recognizes. The constitution's `system_prompt_layers.order` field
// is validated against this set in validate(). Adding a new layer
// here requires adding the corresponding block in
// `internal/llm.BuildSystemPrompt` — the two are coupled.
//
// Keep this list short. Each layer adds tokens to every LLM call
// for every judge. The light constitution uses the minimum
// (identity + tool_directive + footer); the dark constitution uses
// the full set.
var allowedLayers = map[string]bool{
	"identity":              true,
	"authority_declaration": true,
	"refusal_policy":        true,
	"scope":                 true,
	"operational_rules":     true,
	"tone_and_voice":        true,
	"mod_directives":        true,
	"tool_directive":        true,
	"constitution_footer":   true,
}

// AllowedLayer reports whether name is a known layer. Used by
// loader.go::validate to reject unknown layer names in the TOML
// schema, and exposed so the prompt builder can document the
// allowed set in its own package.
func AllowedLayer(name string) bool { return allowedLayers[name] }

// ID returns the canonical "id@version" handle for this constitution.
// Used as the cache key version (in Fase 3) and as the audit column
// value in sdd_evaluations.constitution_id + constitution_version
// (which together reproduce the exact manifest in effect).
func (c *Constitution) ID() string {
	if c == nil {
		return ""
	}
	return c.Meta.ID + "@" + c.Meta.Version
}

// DisplayLabel is a human-readable summary for logs and tool
// responses. Falls back to the id@version handle if Label is empty.
func (c *Constitution) DisplayLabel() string {
	if c == nil {
		return ""
	}
	if c.Meta.Label != "" {
		return c.Meta.Label
	}
	return c.ID()
}

// ParsedJSON returns the Constitution serialized as JSON. Used by
// the store layer to populate the parsed_json column so a future
// version of the loader can read older manifest shapes without
// needing to re-parse the original TOML.
//
// Returns "" if marshaling fails. The caller (typically store.Save)
// treats that as an error.
func (c *Constitution) ParsedJSON() string {
	if c == nil {
		return ""
	}
	b, err := json.Marshal(c)
	if err != nil {
		return ""
	}
	return string(b)
}
