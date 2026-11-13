//go:build !windows

package host

import "errors"

func scheduleWindowsUpdate(staged, target string) error {
	return errors.New("windows update helper is not available on this platform")
}

func scheduleWindowsSelfReplace(staged, target string) error {
	return errors.New("windows self-replacement is not available on this platform")
}
