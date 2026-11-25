//go:build !windows

package host

// EnsureInstallDirOnPath is a no-op outside Windows: the POSIX installers add
// the directory to the user profile themselves.
func EnsureInstallDirOnPath() error {
	return nil
}
