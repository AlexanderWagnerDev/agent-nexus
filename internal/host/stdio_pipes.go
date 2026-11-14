package host

import (
	"os"
	"os/exec"
)

// stdioPipes connects a child's stdout/stderr to plain OS pipes instead of
// exec.StdoutPipe. With StdoutPipe, Wait must not run before the readers reach
// EOF; if a descendant inherits the write end and stays alive, the run would
// never be reported as exited. Plain pipes let Wait run immediately, and the
// reader ends can be closed to release the scanners.
func stdioPipes(cmd *exec.Cmd) (stdout, stderr *os.File, closeWrite func(), err error) {
	outR, outW, err := os.Pipe()
	if err != nil {
		return nil, nil, nil, err
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		outR.Close()
		outW.Close()
		return nil, nil, nil, err
	}
	cmd.Stdout = outW
	cmd.Stderr = errW
	return outR, errR, func() {
		outW.Close()
		errW.Close()
	}, nil
}

func closePipe(f *os.File) {
	if f != nil {
		_ = f.Close()
	}
}
