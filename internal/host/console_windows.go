//go:build windows

package host

import "syscall"

var (
	user32            = syscall.NewLazyDLL("user32.dll")
	procGetConsoleWnd = user32.NewProc("GetConsoleWindow")
	procShowWindow    = user32.NewProc("ShowWindow")
)

// HideConsoleWindow hides the console window of the current process. The Task
// Scheduler starts the daemon with `run --hidden`, so the window never becomes
// a close target for the user. When no console exists it is a no-op.
func HideConsoleWindow() {
	hwnd, _, _ := procGetConsoleWnd.Call()
	if hwnd == 0 {
		return
	}
	const swHide = 0
	_, _, _ = procShowWindow.Call(hwnd, swHide)
}
