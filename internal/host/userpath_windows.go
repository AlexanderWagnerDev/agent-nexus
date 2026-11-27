//go:build windows

package host

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows/registry"
)

var procSendMessageTimeoutW = user32.NewProc("SendMessageTimeoutW")

// EnsureInstallDirOnPath adds the directory of the running binaries to the
// per-user PATH when it is missing. The installer does this once, but an
// update or a manual PATH edit must not leave `nexusd`/`nexus` unresolvable,
// so the daemon checks again on every start.
func EnsureInstallDirOnPath() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return ensureUserPath(filepath.Dir(exe))
}

func ensureUserPath(dir string) error {
	dir = strings.TrimRight(strings.TrimSpace(dir), `\`)
	if dir == "" {
		return errors.New("empty install directory")
	}
	key, err := registry.OpenKey(registry.CURRENT_USER, `Environment`, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	current, kind, err := key.GetStringValue("Path")
	if err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	updated, changed := mergeUserPath(current, dir)
	if !changed {
		return nil
	}
	if kind == registry.EXPAND_SZ {
		if err := key.SetExpandStringValue("Path", updated); err != nil {
			return err
		}
	} else if err := key.SetStringValue("Path", updated); err != nil {
		return err
	}
	broadcastEnvChange()
	return nil
}

// userPathDirs returns the current per-user PATH from the registry. A daemon
// started by a logon task keeps the environment from start time, so a PATH
// entry added later is otherwise invisible to exec.LookPath until it restarts.
func userPathDirs() []string {
	key, err := registry.OpenKey(registry.CURRENT_USER, `Environment`, registry.QUERY_VALUE)
	if err != nil {
		return nil
	}
	defer key.Close()
	value, _, err := key.GetStringValue("Path")
	if err != nil {
		return nil
	}
	var dirs []string
	for _, entry := range strings.Split(expandRegistryPath(value), ";") {
		if entry = strings.Trim(strings.TrimSpace(entry), `"`); entry != "" {
			dirs = append(dirs, entry)
		}
	}
	return dirs
}

// expandRegistryPath expands Windows %VAR% references, which os.ExpandEnv
// does not recognize. Unknown variables stay literal, like the OS behavior.
func expandRegistryPath(value string) string {
	var b strings.Builder
	for {
		start := strings.IndexByte(value, '%')
		if start < 0 {
			b.WriteString(value)
			return b.String()
		}
		end := strings.IndexByte(value[start+1:], '%')
		if end < 0 {
			b.WriteString(value)
			return b.String()
		}
		end += start + 1
		b.WriteString(value[:start])
		if expanded, ok := os.LookupEnv(value[start+1 : end]); ok {
			b.WriteString(expanded)
		} else {
			b.WriteString(value[start : end+1])
		}
		value = value[end+1:]
	}
}

// mergeUserPath reports the updated PATH and whether dir was missing. Entries
// are compared case-insensitively and without trailing backslashes, as Windows
// does for PATH lookups; existing entries and their order are preserved.
func mergeUserPath(current, dir string) (string, bool) {
	for _, entry := range strings.Split(current, ";") {
		if strings.EqualFold(strings.TrimRight(strings.TrimSpace(entry), `\`), dir) {
			return current, false
		}
	}
	if strings.TrimSpace(current) == "" {
		return dir, true
	}
	current = strings.TrimRight(current, "; ")
	if current == "" {
		return dir, true
	}
	return current + ";" + dir, true
}

// broadcastEnvChange tells Explorer about the new user environment so terminals
// opened from the Start menu see the PATH without a sign-out.
func broadcastEnvChange() {
	const (
		hwndBroadcast   = 0xffff
		wmSettingChange = 0x001a
		smtoAbortIfHung = 0x0002
	)
	name, err := syscall.UTF16PtrFromString("Environment")
	if err != nil {
		return
	}
	var result uintptr
	_, _, _ = procSendMessageTimeoutW.Call(hwndBroadcast, wmSettingChange, 0,
		uintptr(unsafe.Pointer(name)), smtoAbortIfHung, 5000, uintptr(unsafe.Pointer(&result)))
}
