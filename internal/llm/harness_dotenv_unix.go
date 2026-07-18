//go:build !windows

// Package llm: Unix user-level env reader.
//
// On Unix, the user-level env is already what `os.Getenv` reads;
// the process inherits the shell's exported vars at start. We
// don't need to read any external file (the operator's $HOME/.env
// is read by loadDotenvFile in harness_dotenv.go, and ~/.profile /
// ~/.bashrc are shell init files, not env storage).
//
// This file exists so the cross-platform build tags stay clean.
package llm

func loadUserLevelEnv(out map[string]string) {
	// No-op on Unix: process env already has user-level vars.
	// The .env loading in harness_dotenv.go handles file-based
	// overrides.
}
