//go:build windows

package host

// secureDirOwner is a no-op on Windows: %TEMP% is per user and ACL-protected.
func secureDirOwner(string) error { return nil }
