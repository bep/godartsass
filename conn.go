package godartsass

import (
	"errors"
	"io"
	"os/exec"
	"time"
)

func newConn(cmd *exec.Cmd) (_ conn, err error) {
	in, err := cmd.StdinPipe()
	if err != nil {
		return conn{}, err
	}
	defer func() {
		if err != nil {
			in.Close()
		}
	}()

	out, err := cmd.StdoutPipe()

	return conn{out, in, cmd}, err
}

// conn wraps a ReadCloser, WriteCloser, and a Cmd.
type conn struct {
	io.ReadCloser
	io.WriteCloser
	cmd *exec.Cmd
}

// Start starts conn's Cmd.
func (c conn) Start() error {
	err := c.cmd.Start()
	if err != nil {
		c.Close()
	}
	return err
}

// Close closes conn's WriteCloser, ReadClosers, and waits for the command to finish.
func (c conn) Close() error {
	writeErr := c.WriteCloser.Close()
	readErr := c.ReadCloser.Close()
	cmdErr := c.waitWithTimeout()

	if writeErr != nil {
		return writeErr
	}

	if readErr != nil {
		return readErr
	}

	return cmdErr

}

// dart-sass-embedded ends on itself on EOF, this is just to give it some
// time to do so.
func (c conn) waitWithTimeout() error {
	result := make(chan error, 1)
	go func() { result <- c.cmd.Wait() }()
	select {
	case err := <-result:
		return err
	case <-time.After(time.Second):
		return errors.New("timed out waiting for dart-sass-embedded to finish")
	}
}
