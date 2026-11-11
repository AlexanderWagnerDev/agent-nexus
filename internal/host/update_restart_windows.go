//go:build windows

package host

import "errors"

func restartUnixService() error {
	return errors.New("unix service restart is not available on windows")
}
