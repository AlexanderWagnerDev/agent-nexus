//go:build !windows

package host

import (
	"fmt"
	"os"
	"syscall"
)

// secureDirOwner refuses a directory that another local user created in a
// shared temporary directory such as /tmp. Without it an attacker could
// pre-create agentnexus-attachments/<run> and make the daemon write prompt
// attachments into a tree they control.
func secureDirOwner(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		return err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}
	if int(st.Uid) != os.Geteuid() {
		return fmt.Errorf("refusing attachment directory %s: it belongs to another user", dir)
	}
	return nil
}
