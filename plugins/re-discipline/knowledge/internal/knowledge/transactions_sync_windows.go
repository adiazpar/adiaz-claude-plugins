//go:build windows

package knowledge

// replaceFile uses MOVEFILE_WRITE_THROUGH on Windows; directory handles do not
// provide the portable fsync operation used on POSIX.
func syncTransactionDirectory(string) error { return nil }
