// Package godartsass provides a Go API for the Dass Sass Embedded protocol.
package godartsass

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"

	"github.com/cli/safeexec"

	"github.com/bep/godartsass/internal/embeddedsass"
	"google.golang.org/protobuf/proto"
)

var defaultDartSassEmbeddedFilename = "dart-sass-embedded"

const (
	// Dart Sass requires a schema of some sort, add this
	// if the resolver does not.
	dummyImportSchema = "godartimport:"

	// There is only one, and this number is picked out of a hat.
	importerID = 5679
)

// Start creates an starts a new SCSS transpiler that communicates with the
// Dass Sass Embedded protocol via Stdin and Stdout.
//
// Closing the transpiler will shut down the process.
//
// Note that the Transpiler is thread safe, and the recommended way of using
// this is to create one and use that for all the SCSS processing needed.
func Start(opts Options) (*Transpiler, error) {
	if err := opts.init(); err != nil {
		return nil, err
	}

	// See https://github.com/golang/go/issues/38736
	bin, err := safeexec.LookPath(opts.DartSassEmbeddedFilename)
	if err != nil {
		return nil, err
	}

	cmd := exec.Command(bin)
	cmd.Stderr = os.Stderr

	conn, err := newConn(cmd)
	if err != nil {
		return nil, err
	}

	if err := conn.Start(); err != nil {
		return nil, err
	}

	t := &Transpiler{
		opts:    opts,
		conn:    conn,
		pending: make(map[uint32]*call),
	}

	go t.input()

	return t, nil
}

// Transpiler controls transpiling of SCSS into CSS.
type Transpiler struct {
	opts Options

	// stdin/stdout of the Dart Sass protocol
	conn io.ReadWriteCloser

	// Protects the sending of the compile request.
	reqMu sync.Mutex

	mu      sync.Mutex // Protects all below.
	seq     uint32
	pending map[uint32]*call
}

// Result holds the result returned from Execute.
type Result struct {
	CSS       string
	SourceMap string
}

// Close closes the stream to the embedded Dart Sass Protocol, which
// shuts down.
func (t *Transpiler) Close() error {
	return t.conn.Close()
}

// Execute transpiles the string Source given in Args into CSS.
func (t *Transpiler) Execute(args Args) (Result, error) {
	var result Result

	if err := args.init(t.opts); err != nil {
		return result, err
	}

	message := &embeddedsass.InboundMessage_CompileRequest_{
		CompileRequest: &embeddedsass.InboundMessage_CompileRequest{
			Importers: args.sassImporters,
			Style:     args.sassOutputStyle,
			Input: &embeddedsass.InboundMessage_CompileRequest_String_{
				String_: &embeddedsass.InboundMessage_CompileRequest_StringInput{
					Syntax: args.sassSourceSyntax,
					Source: args.Source,
				},
			},
			SourceMap: args.EnableSourceMap,
		},
	}

	resp, err := t.invoke(
		&embeddedsass.InboundMessage{
			Message: message,
		},
	)
	if err != nil {
		return result, err
	}

	csp := resp.Message.(*embeddedsass.OutboundMessage_CompileResponse_)

	switch resp := csp.CompileResponse.Result.(type) {
	case *embeddedsass.OutboundMessage_CompileResponse_Success:
		result.CSS = resp.Success.Css
		result.SourceMap = resp.Success.SourceMap
	case *embeddedsass.OutboundMessage_CompileResponse_Failure:
		// TODO1 create a better error: offset, context etc.
		return result, fmt.Errorf("compile failed: %s", resp.Failure.GetMessage())
	default:
		return result, fmt.Errorf("unsupported response type: %T", resp)
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
		case *embeddedsass.OutboundMessage_CanonicalizeRequest_:
			var url *embeddedsass.InboundMessage_CanonicalizeResponse_Url
			if resolved := t.opts.ImportResolver.CanonicalizeURL(c.CanonicalizeRequest.GetUrl()); resolved != "" {
				if !strings.Contains(resolved, ":") {
					// Add a dummy schema.
					resolved = dummyImportSchema + ":" + resolved
				}
				url = &embeddedsass.InboundMessage_CanonicalizeResponse_Url{
					Url: resolved,
				}
			}

			response := &embeddedsass.InboundMessage_CanonicalizeResponse_{
				CanonicalizeResponse: &embeddedsass.InboundMessage_CanonicalizeResponse{
					Id:     c.CanonicalizeRequest.GetId(),
					Result: url,
				},
			}

			t.sendInboundMessage(
				&embeddedsass.InboundMessage{
					Message: response,
				},
			)

		case *embeddedsass.OutboundMessage_ImportRequest_:
			response := &embeddedsass.InboundMessage_ImportResponse_{
				ImportResponse: &embeddedsass.InboundMessage_ImportResponse{
					Id: c.ImportRequest.GetId(),
					Result: &embeddedsass.InboundMessage_ImportResponse_Success{
						Success: &embeddedsass.InboundMessage_ImportResponse_ImportSuccess{
							Contents: t.opts.ImportResolver.Load(strings.TrimPrefix(c.ImportRequest.GetUrl(), dummyImportSchema)),
						},
					},
				},
			}

			t.sendInboundMessage(
				&embeddedsass.InboundMessage{
					Message: response,
				},
			)
		case *embeddedsass.OutboundMessage_LogEvent_:
			// Drop these for now.
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

func (t *Transpiler) invoke(message *embeddedsass.InboundMessage) (*embeddedsass.OutboundMessage, error) {
	request := &call{
		Request: message,
		Done:    make(chan *call, 1),
	}

	response := <-t.sendCall(request).Done

	if response.Error != nil {
		return nil, response.Error
	}

	return response.Response, nil
}

func (t *Transpiler) sendCall(call *call) *call {
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
		call.Error = fmt.Errorf("unsupported request message type. %T", call.Request.Message)
		return call
	}

	call.Error = t.sendInboundMessage(call.Request)

	return call
}

func (t *Transpiler) sendInboundMessage(message *embeddedsass.InboundMessage) error {
	out, err := proto.Marshal(message)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %s", err)
	}

	// Every message must begin with a 4-byte (32-bit) unsigned little-endian
	// integer indicating the length in bytes of the remaining message.
	reqLen := uint32(len(out))

	if err := binary.Write(t.conn, binary.LittleEndian, reqLen); err != nil {
		return err
	}

	n, err := t.conn.Write(out)
	if n != len(out) {
		return errors.New("failed to write payload")
	}
	return err
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

func isWindows() bool {
	return runtime.GOOS == "windows"
}
