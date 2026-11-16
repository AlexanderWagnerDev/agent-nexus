//go:build !windows

package host

// HideConsoleWindow is a no-op outside Windows.
func HideConsoleWindow() {}
