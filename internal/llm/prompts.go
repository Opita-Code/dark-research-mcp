package llm

import (
	"strings"

	"github.com/dark-agents/research-mcp/internal/constitution"
)

// PromptContext is the input to BuildSystemPrompt. It carries the
// resolved constitution (from constitution.Resolve), the per-tool
// directive (the judge's "what to do"), and optional mod directives
// (Fase 2 — empty for now).
//
// BuildSystemPrompt is the single point where the constitution
// affects the actual text the LLM sees. The caller (dark_ssd_*
// handler) does NOT compose the prompt itself; it passes the inputs
// and trusts the builder. This keeps the prompt construction
// centralized so changes to the layering model don't ripple through
// every judge.
type PromptContext struct {
	// Constitution is the resolved active constitution. nil = no
	// constitution active = light behavior (the pre-Fase-1
	// contract). The builder treats this identically to a light
	// constitution with an empty meta.
	Constitution *constitution.Constitution

	// ToolName is the dark_ssd_* tool name. Used to look up the
	// default tool directive when the caller did not pass
	// ToolDirective explicitly. Pass an empty string if the
	// directive is provided in ToolDirective and you don't want
	// the lookup.
	ToolName string

	// ToolDirective is the per-judge default directive. If empty
	// and ToolName is set, the builder looks up the default via
	// DirectiveFor. If both are empty the builder falls back to a
	// generic "You are a judge" prompt.
	ToolDirective string

	// ActiveMods is a list of mod-supplied prompt fragments, in
	// the order they should be rendered. Empty in Fase 1; wired
	// in Fase 2.
	ActiveMods []ModDirective
}

// ModDirective is one mod's contribution to the system prompt.
// Fase 2 will populate these from the mod manifest. For now the
// type is defined so PromptContext is forward-compatible.
type ModDirective struct {
	ModID  string // "user/osint-cve-deepdive"
	Body   string // text fragment
	Source string // "prompt_fragment" | "knowledge" | "directive"
}

// BuildSystemPrompt composes the system prompt from the inputs.
// The composition rules are:
//
//  1. If Constitution is nil OR Constitution.Refusal.Mode is
//     passthrough (or empty), return ToolDirective verbatim. This
//     is the pre-Fase-1 contract: zero change in observable
//     behavior for callers using the default constitution.
//
//  2. If Constitution.Refusal.Mode is "never_refuse", render the
//     layered system prompt declared by the constitution. Layers
//     are rendered in the order declared in
//     Constitution.Layers.Order (which the loader validated against
//     AllowedLayer). Layers marked as "tool_directive" use the
//     ToolDirective value; the rest pull from the constitution
//     itself (currently empty for dark — see dark.toml).
//
// The constitution's scope, tone, identity, etc. are NOT rendered
// by the builder in Fase 1. The dark.toml path renders only the
// minimum layers that change the LLM's behavior: identity,
// authority_declaration, refusal_policy, scope, operational_rules,
// tone_and_voice, mod_directives (empty for now), tool_directive,
// constitution_footer. The full body for each layer is in dark.toml.
//
// Why so much in the TOML and so little in Go? Two reasons:
//
//  1. The constitution is the user's policy. They can edit it
//     without recompiling the binary.
//  2. A future Fase (e.g. a UI to author constitutions) can render
//     previews without booting the full MCP stack.
func BuildSystemPrompt(ctx PromptContext) string {
	if IsLightMode(ctx.Constitution) {
		// Light path: identical to pre-Fase-1. Return the tool
		// directive (looked up if necessary) with no wrapper.
		d := ctx.ToolDirective
		if d == "" {
			d = DirectiveFor(ctx.ToolName)
		}
		if d == "" {
			d = "You are a judge. Respond with JSON only."
		}
		return d
	}

	// Dark (or any "never_refuse") path: layered prompt.
	c := ctx.Constitution
	order := c.Layers.Order
	if len(order) == 0 {
		// Constitution didn't declare an order — fall back to the
		// built-in default order. This keeps a malformed dark.toml
		// from breaking the system, at the cost of a less
		// opinionated prompt.
		order = defaultDarkOrder
	}

	toolDirective := ctx.ToolDirective
	if toolDirective == "" {
		toolDirective = DirectiveFor(ctx.ToolName)
	}

	blocks := make([]ConstitutionLayerBlock, 0, len(order))
	for _, name := range order {
		body := renderLayer(name, c, toolDirective, ctx.ActiveMods)
		if body == "" {
			continue
		}
		blocks = append(blocks, ConstitutionLayerBlock{
			Name:   name,
			Body:   body,
			Source: sourceFor(name, c, ctx.ActiveMods),
		})
	}

	return joinBlocks(blocks)
}

// defaultDarkOrder is the order BuildSystemPrompt uses when the
// constitution doesn't declare one. Matches the comment in the plan
// for the v1.0 constitution. Layers map 1:1 to `AllowedLayer` keys.
var defaultDarkOrder = []string{
	"identity",
	"authority_declaration",
	"refusal_policy",
	"scope",
	"operational_rules",
	"tone_and_voice",
	"mod_directives",
	"tool_directive",
	"constitution_footer",
}

// renderLayer returns the body for one layer. Pulls from the
// constitution's stored layers (dark.toml populates them) or
// synthesizes from the constitution struct fields when the layer
// doesn't have a stored body. The ToolDirective layer is special:
// its body always comes from the per-judge directive, never from
// the constitution.
//
// Returns "" if the layer is not in the constitution AND has no
// synthesized content. The caller (BuildSystemPrompt) skips empty
// bodies so a missing layer in the dark.toml just doesn't appear in
// the prompt — no dangling "<!-- layer: scope -->" placeholders.
func renderLayer(name string, c *constitution.Constitution, toolDirective string, mods []ModDirective) string {
	switch name {
	case "identity":
		return "Identity: " + c.Identity.AgentName + " — " + c.Identity.AgentRole + "."
	case "authority_declaration":
		return c.Authority.Body
	case "refusal_policy":
		return c.Refusal.Body
	case "scope":
		return c.Scope.Body
	case "operational_rules":
		return c.OperationalRules.Body
	case "tone_and_voice":
		return c.Tone.Body
	case "mod_directives":
		if len(mods) == 0 {
			return ""
		}
		var sb strings.Builder
		for _, m := range mods {
			sb.WriteString("Mod directive from ")
			sb.WriteString(m.ModID)
			sb.WriteString(" (")
			sb.WriteString(m.Source)
			sb.WriteString("):\n")
			sb.WriteString(m.Body)
			sb.WriteString("\n\n")
		}
		return strings.TrimRight(sb.String(), "\n")
	case "tool_directive":
		if toolDirective == "" {
			return "You are a judge. Respond with JSON only."
		}
		return toolDirective
	case "constitution_footer":
		return c.Footer.Text
	}
	return ""
}

// sourceFor returns a "source" tag for a layer block — used in
// audit logs and in the constitution_diff tool (future) to show
// where each line of the system prompt came from.
func sourceFor(name string, c *constitution.Constitution, mods []ModDirective) string {
	switch name {
	case "mod_directives":
		return "mod:" + firstModID(mods)
	case "tool_directive":
		return "tool:judge"
	}
	return "constitution"
}

// firstModID returns the first mod's id, or "" if none. Used only
// for the Source tag on the mod_directives layer.
func firstModID(mods []ModDirective) string {
	if len(mods) == 0 {
		return ""
	}
	return mods[0].ModID
}

// joinBlocks concatenates the layer blocks with a blank line
// between each. The blank line is important: the LLM reads the
// system prompt as a series of paragraphs and uses paragraph
// breaks to infer where one directive ends and the next begins.
func joinBlocks(blocks []ConstitutionLayerBlock) string {
	var sb strings.Builder
	for i, b := range blocks {
		if i > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString(b.Body)
	}
	return sb.String()
}
