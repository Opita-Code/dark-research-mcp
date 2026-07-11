package mods

import (
	"path/filepath"
	"testing"
)

// TestLoad_exampleMods_ParsesWithoutError is the integration
// smoke test: every example mod in /mods-examples must load
// cleanly. This catches TOML typos and schema drift before
// users see them.
func TestLoad_exampleMods_ParsesWithoutError(t *testing.T) {
	// The path is computed relative to the test working dir,
	// which is the package directory. We step up to the repo
	// root to find mods-examples.
	modDirs := []string{
		filepath.Join("..", "..", "mods-examples", "osint-cve-deepdive"),
		filepath.Join("..", "..", "mods-examples", "red-team-jailbreak-arsenal"),
	}
	for _, dir := range modDirs {
		t.Run(dir, func(t *testing.T) {
			loaded, err := Load(dir)
			if err != nil {
				t.Fatalf("Load(%q): %v", dir, err)
			}
			if loaded.Manifest.Meta.ID == "" {
				t.Error("loaded mod has empty meta.id")
			}
			if loaded.SHA256 == "" {
				t.Error("loaded mod has empty SHA256")
			}
			if len(loaded.Knowledge) == 0 && len(loaded.Directives) == 0 {
				t.Error("loaded mod has no knowledge or directives (did the loader read the files?)")
			}
		})
	}
}
