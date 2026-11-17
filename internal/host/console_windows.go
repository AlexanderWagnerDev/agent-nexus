//go:build windows

package host

import "syscall"

var (
	kernel32          = syscall.NewLazyDLL("kernel32.dll")
	user32            = syscall.NewLazyDLL("user32.dll")
	procGetConsoleWnd = kernel32.NewProc("GetConsoleWindow")
	procShowWindow    = user32.NewProc("ShowWindow")
	procFreeConsole   = kernel32.NewProc("FreeConsole")
)

// HideConsoleWindow hides and then detaches the daemon's console. The Task
// Scheduler starts the daemon with `run --hidden`; at logon it can create the
// console before the desktop is ready, where ShowWindow loses the race and the
// window stays visible. FreeConsole removes the window entirely, so it can
// neither appear nor be closed by accident.
func HideConsoleWindow() {
	hwnd, _, _ := procGetConsoleWnd.Call()
	if hwnd == 0 {
		return
	}
	const swHide = 0
	_, _, _ = procShowWindow.Call(hwnd, swHide)
	_, _, _ = procFreeConsole.Call()
}
