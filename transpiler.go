// Package godartsass provides a Go API for the Dass Sass Embedded protocol.
package godartsass

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"sync"

	"github.com/cli/safeexec"

	"github.com/bep/godartsass/internal/embeddedsass"
	"google.golang.org/protobuf/proto"
)

var defaultDartSassEmbeddedFilename = "dart-sass-embedded"

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

	// Protects the sending of messages to Dart Sass.
	sendMu sync.Mutex

	mu      sync.Mutex // Protects all below.
	seq     uint32
	pending map[uint32]*call
}

// Result holds the result returned from Execute.
type Result struct {
	CSS       string
	SourceMap string
}

// SassError is the error returned from Execute on compile errors.
type SassError struct {
	Message string `json:"message"`
	Span    struct {
		Text  string `json:"text"`
		Start struct {
			Offset int `json:"offset"`
			Column int `json:"column"`
		} `json:"start"`
		End struct {
			Offset int `json:"offset"`
			Column int `json:"column"`
		} `json:"end"`
		Context string `json:"context"`
	} `json:"span"`
}

func (e SassError) Error() string {
	return e.Message
}

// Close closes the stream to the embedded Dart Sass Protocol, which
// shuts down.
func (t *Transpiler) Close() error {
	return t.conn.Close()
}

// Execute transpiles the string Source given in Args into CSS.
// If Dart Sass resturns a "compile failure", the error returned will be
// of type SassError..
func (t *Transpiler) Execute(args Args) (Result, error) {
	var result Result

	createInboundMessage := func(seq uint32) (*embeddedsass.InboundMessage, error) {
		if err := args.init(seq, t.opts); err != nil {
			return nil, err
		}

		message := &embeddedsass.InboundMessage_CompileRequest_{
			CompileRequest: &embeddedsass.InboundMessage_CompileRequest{
				Importers: args.sassImporters,
				Style:     args.sassOutputStyle,
				Input: &embeddedsass.InboundMessage_CompileRequest_String_{
					String_: &embeddedsass.InboundMessage_CompileRequest_StringInput{
						Syntax: args.sassSourceSyntax,
						Source: args.Source,
						Url:    args.URL,
					},
				},
				SourceMap: args.EnableSourceMap,
			},
		}

		return &embeddedsass.InboundMessage{
			Message: message,
		}, nil
	}

	call, err := t.newCall(createInboundMessage, args)
	if err != nil {
		return result, err
	}
	call = <-call.Done
	if call.Error != nil {
		return result, call.Error
	}

	response := call.Response
	csp := response.Message.(*embeddedsass.OutboundMessage_CompileResponse_)

	switch resp := csp.CompileResponse.Result.(type) {
	case *embeddedsass.OutboundMessage_CompileResponse_Success:
		result.CSS = resp.Success.Css
		result.SourceMap = resp.Success.SourceMap
	case *embeddedsass.OutboundMessage_CompileResponse_Failure:
		asJson, err := json.Marshal(resp.Failure)
		if err != nil {
			return result, err
		}
		var sassErr SassError
		err = json.Unmarshal(asJson, &sassErr)
		if err != nil {
			return result, err
		}
		return result, sassErr
	default:
		return result, fmt.Errorf("unsupported response type: %T", resp)
	}

	return result, nil
}

func (t *Transpiler) getImportResolver(id uint32) ImportResolver {
	t.mu.Lock()
	defer t.mu.Unlock()
	call, found := t.pending[id]
	if !found {
		panic(fmt.Sprintf("call with ID %d not found", id))
	}
	return call.importResolver
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
			resolver := t.getImportResolver(c.CanonicalizeRequest.CompilationId)
			var url *embeddedsass.InboundMessage_CanonicalizeResponse_Url
			if resolved := resolver.CanonicalizeURL(c.CanonicalizeRequest.GetUrl()); resolved != "" {
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
			resolver := t.getImportResolver(c.ImportRequest.CompilationId)
			url := c.ImportRequest.GetUrl()
			var sourceMapURL string
			// Dart Sass expect a browser-accessible URL or an empty string.
			// If no URL is supplied, a `data:` URL wil be generated
			// automatically from `contents`
			// The hasSchema function may be too coarse grained, but we
			// need to test this in real life situations.
			if hasSchema(url) {
				sourceMapURL = url
			}

			response := &embeddedsass.InboundMessage_ImportResponse_{
				ImportResponse: &embeddedsass.InboundMessage_ImportResponse{
					Id: c.ImportRequest.GetId(),
					Result: &embeddedsass.InboundMessage_ImportResponse_Success{
						Success: &embeddedsass.InboundMessage_ImportResponse_ImportSuccess{
							Contents:     resolver.Load(url),
							SourceMapUrl: sourceMapURL,
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

func (t *Transpiler) newCall(createInbound func(seq uint32) (*embeddedsass.InboundMessage, error), args Args) (*call, error) {
	t.mu.Lock()
	// TODO1 handle shutdown.
	id := t.seq
	req, err := createInbound(id)
	if err != nil {
		t.mu.Unlock()
		return nil, err
	}

	call := &call{
		Request:        req,
		Done:           make(chan *call, 1),
		importResolver: args.ImportResolver,
	}

	t.pending[id] = call
	t.seq++
	t.mu.Unlock()

	switch c := call.Request.Message.(type) {
	case *embeddedsass.InboundMessage_CompileRequest_:
		c.CompileRequest.Id = id
	default:
		return nil, fmt.Errorf("unsupported request message type. %T", call.Request.Message)
	}

	return call, t.sendInboundMessage(call.Request)
}

func (t *Transpiler) sendInboundMessage(message *embeddedsass.InboundMessage) error {
	t.sendMu.Lock()
	defer t.sendMu.Unlock()

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
	Request        *embeddedsass.InboundMessage
	Response       *embeddedsass.OutboundMessage
	importResolver ImportResolver

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

var hasSchemaRe = regexp.MustCompile("^[a-z]*:")

func hasSchema(s string) bool {
	return hasSchemaRe.MatchString(s)
}
