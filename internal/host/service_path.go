package host

import (
	"html"
	"os"
	"path"
	"strings"
)

// servicePath gives a per-user service the same useful PATH an interactive
// shell has for agent binaries (npm global, ~/.local/bin, Homebrew), without
// inheriting the caller's full environment. It is POSIX-only by design.
func servicePath(home string) string {
	parts := []string{
		path.Join(home, ".local", "bin"),
		path.Join(home, "bin"),
		path.Join(home, ".npm-global", "bin"),
		"/usr/local/bin",
		"/opt/homebrew/bin",
		"/usr/bin",
		"/bin",
		"/usr/sbin",
		"/sbin",
	}
	return strings.Join(parts, string(os.PathListSeparator))
}

func xmlEscapeText(value string) string { return html.EscapeString(value) }
