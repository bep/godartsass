package godartsass

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"
)

// This code is borrowed from https://github.com/natefinch/pie
//
// MIT License, copyright Nate Finch.
//
// TODO(bep) consider upstream.

// start runs the plugin and returns an ioPipe that can be used to control the
// plugin.
func start(cmd commander) (_ ioPipe, err error) {
	in, err := cmd.StdinPipe()
	if err != nil {
		return ioPipe{}, err
	}
	defer func() {
		if err != nil {
			in.Close()
		}
	}()
	out, err := cmd.StdoutPipe()
	if err != nil {
		return ioPipe{}, err
	}
	defer func() {
		if err != nil {
			out.Close()
		}
	}()

	proc, err := cmd.Start()
	if err != nil {
		return ioPipe{}, err
	}
	return ioPipe{out, in, proc}, nil
}

// makeCommand is a function that just creates an exec.Cmd and the process in
// it. It exists to facilitate testing.
var makeCommand = func(w io.Writer, path string, args []string) commander {
	cmd := exec.Command(path, args...)
	cmd.Stderr = w
	return execCmd{cmd}
}

type execCmd struct {
	*exec.Cmd
}

func (e execCmd) Start() (osProcess, error) {
	if err := e.Cmd.Start(); err != nil {
		return nil, err
	}
	return e.Cmd.Process, nil
}

// commander is an interface that is fulfilled by exec.Cmd and makes our testing
// a little easier.
type commander interface {
	StdinPipe() (io.WriteCloser, error)
	StdoutPipe() (io.ReadCloser, error)
	// Start is like exec.Cmd's start, except it also returns the os.Process if
	// start succeeds.
	Start() (osProcess, error)
}

// osProcess is an interface that is fullfilled by *os.Process and makes our
// testing a little easier.
type osProcess interface {
	Wait() (*os.ProcessState, error)
	Kill() error
	Signal(os.Signal) error
}

// ioPipe simply wraps a ReadCloser, WriteCloser, and a Process, and coordinates
// them so they all close together.
type ioPipe struct {
	io.ReadCloser
	io.WriteCloser
	proc osProcess
}

// Close closes the pipe's WriteCloser, ReadClosers, and process.
func (iop ioPipe) Close() error {
	err := iop.ReadCloser.Close()
	if writeErr := iop.WriteCloser.Close(); writeErr != nil {
		err = writeErr
	}
	if procErr := iop.closeProc(); procErr != nil {
		err = procErr
	}
	return err
}

var errProcStopTimeout = errors.New("process killed after timeout waiting for process to stop")

// procTimeout is the timeout to wait for a process to stop after being
// signalled.  It is adjustable to keep tests fast.
var procTimeout = time.Second

// closeProc sends an interrupt signal to the pipe's process, and if it doesn't
// respond in one second, kills the process.
func (iop ioPipe) closeProc() error {
	result := make(chan error, 1)
	go func() { _, err := iop.proc.Wait(); result <- err }()
	if err := iop.proc.Signal(os.Interrupt); err != nil {
		return err
	}
	select {
	case err := <-result:
		return err
	case <-time.After(procTimeout):
		if err := iop.proc.Kill(); err != nil {
			return fmt.Errorf("error killing process after timeout: %s", err)
		}
		return errProcStopTimeout
	}
}
