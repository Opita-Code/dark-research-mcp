//go:build allow_builtin_dark

package constitution

import _ "embed"

// This file is the build-tag-gated wire-up of loadDark. It exists
// only when the binary is built with `-tags allow_builtin_dark`.
// The public release build does NOT include this file, so
// loadDark stays nil (declared in loader.go) and Dark stays nil —
// the runtime cannot resolve "dark" to a built-in.
//
// To embed the dark constitution, build with:
//
//	go build -tags allow_builtin_dark ./cmd/dark-research-mcp
//
// The init() function here runs as part of the package init
// phase. The Go spec says init() functions run AFTER all package-
// level variables are initialized, in file-name lexical order.
// So loader.go's init() runs first (it parses Light), then
// loader_dark.go's init() runs and assigns loadDark. After that
// the package is fully initialized; main() then calls
// constitution.Initialize() to parse the dark bytes into the
// Dark global.

//go:embed constitutions/dark.toml
var darkFS []byte

func init() {
	loadDark = func() []byte { return darkFS }
}
