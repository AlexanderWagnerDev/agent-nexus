//go:build windows

package host

import (
	"os/exec"
	"unsafe"

	"golang.org/x/sys/windows"
)

// processGuard puts the agent into a Job Object configured with
// JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE, so closing the job (or terminating the
// daemon) takes every descendant with it. The assignment happens immediately
// after CreateProcess; a grandchild that is spawned before AssignProcessToJobObject
// returns could in theory escape the job. This window is small but not zero,
// and is documented as a residual risk.
type processGuard struct {
	job windows.Handle
}

func newProcessGuard() *processGuard { return &processGuard{} }

func (g *processGuard) Attach(pid int) error {
	if g.job != 0 {
		return nil
	}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		windows.CloseHandle(job)
		return err
	}
	proc, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		windows.CloseHandle(job)
		return err
	}
	defer windows.CloseHandle(proc)
	if err := windows.AssignProcessToJobObject(job, proc); err != nil {
		windows.CloseHandle(job)
		return err
	}
	g.job = job
	return nil
}

func (g *processGuard) Terminate() {
	if g.job != 0 {
		_ = windows.TerminateJobObject(g.job, 1)
	}
}

func (g *processGuard) Kill() { g.Terminate() }

func (g *processGuard) Close() {
	if g.job != 0 {
		_ = windows.CloseHandle(g.job)
		g.job = 0
	}
}

// configureProcessGroup is a no-op on Windows; the Job Object provides the
// tree containment.
func configureProcessGroup(*exec.Cmd) {}

// processAlive reports whether pid still exists.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(proc)
	var code uint32
	if err := windows.GetExitCodeProcess(proc, &code); err != nil {
		return false
	}
	return code == 259 // STILL_ACTIVE
}
