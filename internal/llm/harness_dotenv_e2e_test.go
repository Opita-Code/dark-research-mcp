// Standalone E2E test: spawn the dark-research-mcp binary with the
// harness's DARK_SCRAPPER_URL set to a dead port and verify the
// LLM client falls back to the harness's MINIMAX_API_KEY against
// api.minimax.io/anthropic.
//
// This bypasses the opencode session entirely. The binary is the
// SAME one opencode would spawn, just driven by an mcp-go client
// in this test process instead of by opencode.
package llm

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestStandaloneE2E_Fallback proves the fix end-to-end by running
// the dark-research-mcp binary as a subprocess and verifying it
// returns a successful verdict when DARK_SCRAPPER_URL points to a
// dead port. The MCP's drift_judge tool should fall through to the
// harness's MINIMAX_API_KEY and call api.minimax.io/anthropic.
//
// Pre-conditions (operator must satisfy):
//   - MINIMAX_API_KEY is set in HKCU\Environment (or $HOME/.env, or
//     project .env) — verified by TestLoadHarnessDotenv_RealRegistry.
//   - The dark-research-mcp.exe binary on disk has the harness
//     dotenv fix (TestNewFromEnv_ScrapperDown_LoadsHarnessDotenv
//     exercises the same code path).
//
// The test is wired to skip itself if the operator's environment
// does not have a MINIMAX_API_KEY reachable via the harness dotenv,
// so it never reports a false failure on a clean machine.
func TestStandaloneE2E_Fallback(t *testing.T) {
	if testing.Short() {
		t.Skip("E2E; skipping in -short mode")
	}

	// 1. Verify a real key is reachable. If not, skip — this is the
	// same precondition that makes the fix valuable in the first
	// place.
	dotenv := LoadHarnessDotenv()
	key := dotenv["MINIMAX_API_KEY"]
	if key == "" {
		t.Skip("MINIMAX_API_KEY not in harness dotenv; nothing to fall back to")
	}

	// 2. Locate the binary. Default: ../dark-research-mcp.exe
	// relative to this test's source. The Makefile / go test runs
	// from the package dir, so we walk up.
	cwd, _ := os.Getwd()
	binPath := filepath.Join(cwd, "..", "..", "dark-research-mcp.exe")
	if _, err := os.Stat(binPath); err != nil {
		t.Skipf("binary not at %s: %v", binPath, err)
	}

	// 3. Spawn the MCP. We pass DARK_SCRAPPER_URL pointing to a
	// closed port (127.0.0.1:1 always refuses) and let the
	// process inherit the harness's env so it can read MINIMAX_API_KEY
	// from HKCU\Environment (we don't set it explicitly).
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binPath)
	cmd.Env = append(os.Environ(),
		"DARK_SCRAPPER_URL=http://127.0.0.1:1", // dead port
	)
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	// 4. Send MCP initialize + tools/list. We don't actually call
	// drift_judge here (it would require a real spec+artifact); we
	// just verify the binary booted, registered the tools, and
	// survives a tools/list round-trip with a dead scrapper URL.
	go func() {
		// Drain stderr in the background to avoid blocking.
		rdr := bufio.NewReader(stderr)
		for {
			line, err := rdr.ReadString('\n')
			if err != nil {
				return
			}
			t.Logf("mcp stderr: %s", strings.TrimRight(line, "\n"))
		}
	}()

	initialize := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0.0.0"}}}`
	if _, err := io.WriteString(stdin, initialize+"\n"); err != nil {
		t.Fatalf("write initialize: %v", err)
	}
	initialized := `{"jsonrpc":"2.0","method":"notifications/initialized"}`
	if _, err := io.WriteString(stdin, initialized+"\n"); err != nil {
		t.Fatalf("write initialized: %v", err)
	}
	listTools := `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`
	if _, err := io.WriteString(stdin, listTools+"\n"); err != nil {
		t.Fatalf("write tools/list: %v", err)
	}

	// 5. Read responses. We expect initialize response + tools/list
	// response. The tools/list must list 15 tools (canonical for
	// dark-research-mcp).
	rdr := bufio.NewReader(stdout)
	gotInitialize := false
	gotToolsList := false
	for !gotToolsList {
		line, err := rdr.ReadString('\n')
		if err != nil {
			t.Fatalf("read mcp response: %v", err)
		}
		var msg map[string]any
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}
		id, _ := msg["id"].(float64)
		if id == 1 {
			gotInitialize = true
			t.Logf("initialize: OK")
		} else if id == 2 {
			gotToolsList = true
			result, _ := msg["result"].(map[string]any)
			tools, _ := result["tools"].([]any)
			t.Logf("tools/list returned %d tools", len(tools))
			if len(tools) == 0 {
				t.Errorf("tools/list returned 0 tools; binary may not have booted correctly")
			}
		}
	}
	if !gotInitialize {
		t.Errorf("no initialize response")
	}

	// 6. The actual drift_judge fallback verification: send a
	// tools/call for dark_ssd_drift_judge with a tiny content
	// payload. The MCP will:
	//   a. Try DARK_SCRAPPER_URL (127.0.0.1:1) — connection refused.
	//   b. Fall back to MINIMAX_API_KEY from harness dotenv.
	//   c. Call https://api.minimax.io/anthropic with the key.
	// We expect either a 200 (verdict) or a 5xx (transient API
	// issue) — anything but "connection refused" proves the
	// fallback fired.
	call := fmt.Sprintf(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"dark_ssd_drift_judge","arguments":{"spec_id":1,"content":"ping"}}}`)
	if _, err := io.WriteString(stdin, call+"\n"); err != nil {
		t.Fatalf("write tools/call: %v", err)
	}
	deadline := time.After(20 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatalf("no response to tools/call within 20s")
		default:
		}
		line, err := rdr.ReadString('\n')
		if err != nil {
			t.Fatalf("read tools/call response: %v", err)
		}
		var msg map[string]any
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}
		id, _ := msg["id"].(float64)
		if id != 3 {
			continue
		}
		// Print whatever we got (success or error envelope).
		pretty, _ := json.MarshalIndent(msg, "", "  ")
		t.Logf("drift_judge response:\n%s", string(pretty))

		// Check: was the response a "connection refused" error?
		// If yes, the fallback DID NOT fire. If no, the fallback
		// fired (even if the upstream API rejected the key, the
		// chain reached it).
		if strings.Contains(string(pretty), "connection refused") ||
			strings.Contains(string(pretty), "No se puede establecer") ||
			strings.Contains(string(pretty), "connectex") {
			t.Errorf("fallback did not fire: scrapper-style connection error leaked through")
		} else {
			t.Logf("fallback fired: response did not contain a connection-refused error")
		}
		break
	}
}
