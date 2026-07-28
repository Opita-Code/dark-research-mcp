// server_test.go — cx.v3 conformance tests for dark-research-mcp v0.8.0.
//
// Per BRIDGE_AND_COEXISTENCE.md v2.0.0 §5.4 test #4:
// "initialize from dark-research-mcp declares coexistence_group=
// dark-agents/research (NEW) and policy_gateway=false".
//
// mcp-go v0.56.0's MCPServer stores `instructions` as an unexported
// field, so we cannot inspect the wire response directly from this
// package. Instead we assert the format string returned by the
// exported BuildInstructions helper, which is what the MCP server
// bakes into the initialize envelope.
package server

import (
	"strings"
	"testing"
)

// TestBuildInstructions_DeclaresCoexistenceGroup verifies that the
// initialize envelope includes the cx.v3 coexistence_group.
func TestBuildInstructions_DeclaresCoexistenceGroup(t *testing.T) {
	got := BuildInstructions("0.8.0-test")
	want := "coexistence_group=dark-agents/research"
	if !strings.Contains(got, want) {
		t.Errorf("BuildInstructions missing %q\n  got: %s", want, got)
	}
}

// TestBuildInstructions_DeclaresPolicyGateway verifies that the
// initialize envelope declares policy_gateway=false (dark-research is
// a backing, NOT a gateway — see BRIDGE §3.2).
func TestBuildInstructions_DeclaresPolicyGateway(t *testing.T) {
	got := BuildInstructions("0.8.0-test")
	want := "policy_gateway=false"
	if !strings.Contains(got, want) {
		t.Errorf("BuildInstructions missing %q\n  got: %s", want, got)
	}

	// Defensive: assert that policy_gateway=true is NOT present
	// (would mean we accidentally declared ourselves a gateway,
	// which would be a conformance bug per BRIDGE §2.1).
	if strings.Contains(got, "policy_gateway=true") {
		t.Errorf("BuildInstructions wrongly contains policy_gateway=true (dark-research must NOT advertise itself as a gateway)\n  got: %s", got)
	}
}

// TestBuildInstructions_MentionsDarkMemoryAsSuccessor verifies that
// the initialize envelope steers operators toward dark-memory for
// the deprecated dark_mem_* namespace.
func TestBuildInstructions_MentionsDarkMemoryAsSuccessor(t *testing.T) {
	got := BuildInstructions("0.8.0-test")
	want := "use dark_memory_* from dark-memory-mcp"
	if !strings.Contains(got, want) {
		t.Errorf("BuildInstructions missing %q (operators need to know dark_mem_* is migrated to dark_memory_*)\n  got: %s", want, got)
	}
}

// TestBuildInstructions_IncludesVersion ensures the version string
// the caller passed is reflected in the instructions (useful for
// harness-side debug surfacing).
func TestBuildInstructions_IncludesVersion(t *testing.T) {
	const ver = "0.8.0-unittest"
	got := BuildInstructions(ver)
	want := "Version=" + ver
	if !strings.Contains(got, want) {
		t.Errorf("BuildInstructions missing %q\n  got: %s", want, got)
	}
}

// TestBuildInstructions_StampsCxV3 verifies the BRIDGE spec reference
// (spec 164 bridge.2 cx.v3) is present, so harness implementers
// grepping for the spec version can find it.
func TestBuildInstructions_StampsCxV3(t *testing.T) {
	got := BuildInstructions("0.8.0-test")
	want := "spec 164 bridge.2 cx.v3"
	if !strings.Contains(got, want) {
		t.Errorf("BuildInstructions missing %q (harness grep target)\n  got: %s", want, got)
	}
}
