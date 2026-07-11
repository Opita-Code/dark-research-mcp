package constitution

import "os"

// writeFile is a tiny helper used by loader_test.go to write bytes
// to a path. Kept here (rather than in the test file) so the test
// file is purely assertions.
func writeFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0o644)
}
