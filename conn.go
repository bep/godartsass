package godartsass

import (
	"bytes"
	"errors"
	"io"
	"os/exec"
	"regexp"
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
	stdErr := &tailBuffer{limit: 1024}
	c := conn{out, in, stdErr, cmd}
	cmd.Stderr = c.stdErr

	return c, err
}

// conn wraps a ReadCloser, WriteCloser, and a Cmd.
type conn struct {
	io.ReadCloser
	io.WriteCloser
	stdErr *tailBuffer
	cmd    *exec.Cmd
}

// Start starts conn's Cmd.
func (c conn) Start() error {
	err := c.cmd.Start()
	if err != nil {
		return c.Close()
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

var brokenPipeRe = regexp.MustCompile("Broken pipe|pipe is being closed")

// dart-sass-embedded ends on itself on EOF, this is just to give it some
// time to do so.
func (c conn) waitWithTimeout() error {
	result := make(chan error, 1)
	go func() { result <- c.cmd.Wait() }()
	select {
	case err := <-result:
		if _, ok := err.(*exec.ExitError); ok {
			if brokenPipeRe.MatchString(c.stdErr.String()) {
				return nil
			}
		}
		return err
	case <-time.After(time.Second):
		return errors.New("timed out waiting for dart-sass-embedded to finish")
	}
}

type tailBuffer struct {
	limit int
	bytes.Buffer
}

func (b *tailBuffer) Write(p []byte) (n int, err error) {
	if len(p)+b.Buffer.Len() > b.limit {
		b.Reset()
	}
	n, err = b.Buffer.Write(p)
	return
}
