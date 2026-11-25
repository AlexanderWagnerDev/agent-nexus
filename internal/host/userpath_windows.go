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
