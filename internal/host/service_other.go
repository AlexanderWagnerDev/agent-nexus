//go:build !windows

package host

import "fmt"

func windowsTaskService(action, exe string) error {
	return fmt.Errorf("windows task service is not available on this platform")
}
