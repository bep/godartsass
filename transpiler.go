package godartsass

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/bep/godartsass/internal/embeddedsass"
	"google.golang.org/protobuf/proto"
)

const (
	defaultDartSassEmbeddedFilename = "dart-sass-embedded"
)

// Start creates an starts a new SCSS transpiler that communicates with the
// Dass Sass Embedded protocol via Stdin and Stdout.
//
// Closing the transpiler will shut down the process and communication.
func Start(opts Options) (*Transpiler, error) {
	if opts.DartSassEmbeddedFilename == "" {
		opts.DartSassEmbeddedFilename = defaultDartSassEmbeddedFilename
	}

	pipe, err := start(makeCommand(os.Stderr, opts.DartSassEmbeddedFilename, []string{}))
	if err != nil {
		return nil, err
	}

	t := &Transpiler{
		opts: opts,
		conn: pipe,
		//conn:    debugReadWriteCloser{pipe},
		pending: make(map[uint32]*call),
	}

	go t.input()

	return t, nil
}

type Options struct {
	// The path to the Dart Sass wrapper binary, an absolute filename
	// if not in $PATH.
	// If this is not set, we will try 'dart-sass-embedded' in $PATH.
	// There may be several ways to install this, one would be to
	// download it from here: https://github.com/sass/dart-sass-embedded/releases
	DartSassEmbeddedFilename string
}

type Result struct {
	CSS string
}

type Transpiler struct {
	opts Options

	conn io.ReadWriteCloser

	reqMu sync.Mutex

	mu      sync.Mutex
	seq     uint32
	pending map[uint32]*call
}

func (c *Transpiler) Close() error {
	return c.conn.Close()
}

func (c *Transpiler) Execute(src string) (Result, error) {
	var result Result

	req := &embeddedsass.InboundMessage_CompileRequest_{
		CompileRequest: &embeddedsass.InboundMessage_CompileRequest{
			Input: &embeddedsass.InboundMessage_CompileRequest_String_{
				String_: &embeddedsass.InboundMessage_CompileRequest_StringInput{
					Source: src,
					// TODO1: importers etc.
				},
			},
		},
	}

	request := &call{
		Request: &embeddedsass.InboundMessage{
			Message: req,
		},
		Done: make(chan *call, 1),
	}

	response := <-c.send(request).Done

	if response.Error != nil {
		return result, response.Error
	}

	resp := response.Response.Message.(*embeddedsass.OutboundMessage_CompileResponse_)

	if resp, ok := resp.CompileResponse.Result.(*embeddedsass.OutboundMessage_CompileResponse_Success); ok {
		result.CSS = resp.Success.Css
	} else {
		// TODO1
	}

	return result, nil
}

func (t *Transpiler) input() {
	var err error

	for err == nil {
		// The header is the length in bytes of the remaining message.
		var plen int32
		err = binary.Read(t.conn, binary.LittleEndian, &plen)
		if err != nil {
			break
		}

		b := make([]byte, plen)

		_, err = io.ReadFull(t.conn, b)
		if err != nil {
			break
		}

		var msg embeddedsass.OutboundMessage

		if err = proto.Unmarshal(b, &msg); err != nil {
			break
		}

		switch c := msg.Message.(type) {
		case *embeddedsass.OutboundMessage_CompileResponse_:
			// Type mismatch, see https://github.com/sass/embedded-protocol/issues/36
			id := uint32(c.CompileResponse.Id)
			// Attach it to the correct pending call.
			t.mu.Lock()
			call := t.pending[id]
			delete(t.pending, id)
			t.mu.Unlock()
			if call == nil {
				err = fmt.Errorf("call with ID %d not found", id)
				break
			}
			call.Response = &msg
			call.done()
		case *embeddedsass.OutboundMessage_Error:
			err = fmt.Errorf("SASS error: %s", c.Error.GetMessage())
		default:
			err = fmt.Errorf("unsupported response message type. %T", msg.Message)
		}

	}

	for _, call := range t.pending {
		call.Error = err
		call.done()
	}
}

func (t *Transpiler) send(call *call) *call {
	t.reqMu.Lock()
	defer t.reqMu.Unlock()

	t.mu.Lock()
	// TODO1 handle shutdown.
	id := t.seq
	t.pending[id] = call
	t.seq++
	t.mu.Unlock()

	switch c := call.Request.Message.(type) {
	case *embeddedsass.InboundMessage_CompileRequest_:
		c.CompileRequest.Id = id
	default:
		call.Error = fmt.Errorf("unsupported message type. %T", call.Request.Message)
		return call
	}

	out, err := proto.Marshal(call.Request)
	if err != nil {
		call.Error = fmt.Errorf("failed to marshal request: %s", err)
		return call
	}

	// Every message must begin with a 4-byte (32-bit) unsigned little-endian
	// integer indicating the length in bytes of the remaining message.
	reqLen := uint32(len(out))

	if err := binary.Write(t.conn, binary.LittleEndian, reqLen); err != nil {
		call.Error = err
		return call
	}

	n, err := t.conn.Write(out)
	if n != len(out) {
		call.Error = errors.New("failed to write payload")
		return call
	}

	call.Error = err

	return call
}

type call struct {
	Request  *embeddedsass.InboundMessage
	Response *embeddedsass.OutboundMessage

	Error error
	Done  chan *call
}

func (call *call) done() {
	select {
	case call.Done <- call:
	default:
	}
}
