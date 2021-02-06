// Package godartsass provides a Go API for the Dass Sass Embedded protocol.
//
// Use the Start function to create and start a new thread safe transpiler.
// Close it when done.
package godartsass

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"strings"
	"time"

	"os"
	"os/exec"
	"sync"

	"github.com/cli/safeexec"

	"github.com/bep/godartsass/internal/embeddedsass"
	"google.golang.org/protobuf/proto"
)

const defaultDartSassEmbeddedFilename = "dart-sass-embedded"

// ErrShutdown will be returned from Execute and Close if the transpiler is or
// is about to be shut down.
var ErrShutdown = errors.New("connection is shut down")

// Start creates and starts a new SCSS transpiler that communicates with the
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
		lenBuf:  make([]byte, binary.MaxVarintLen64),
		pending: make(map[uint32]*call),
	}

	go t.input()

	return t, nil
}

// Transpiler controls transpiling of SCSS into CSS.
type Transpiler struct {
	opts Options

	// stdin/stdout of the Dart Sass protocol
	conn   byteReadWriteCloser
	lenBuf []byte
	msgBuf []byte

	closing  bool
	shutdown bool

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
		Url     string `json:"url"`
		Context string `json:"context"`
	} `json:"span"`
}

func (e SassError) Error() string {
	span := e.Span
	file := path.Clean(strings.TrimPrefix(span.Url, "file:"))
	return fmt.Sprintf("file: %q, context: %q: %s", file, span.Context, e.Message)
}

// Close closes the stream to the embedded Dart Sass Protocol, shutting it down.
// If it is already shutting down, ErrShutdown is returned.
func (t *Transpiler) Close() error {
	t.sendMu.Lock()
	defer t.sendMu.Unlock()
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closing {
		return ErrShutdown
	}

	t.closing = true
	err := t.conn.Close()

	return err
}

// Execute transpiles the string Source given in Args into CSS.
// If Dart Sass resturns a "compile failure", the error returned will be
// of type SassError.
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

	select {
	case call = <-call.Done:
	case <-time.After(t.opts.Timeout):
		return result, errors.New("timeout waiting for Dart Sass to respond; if you're running with Embedded Sass protocol < beta6, you need to upgrade")
	}

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

func (t *Transpiler) getCall(id uint32) *call {
	t.mu.Lock()
	defer t.mu.Unlock()
	call, found := t.pending[id]
	if !found {
		panic(fmt.Sprintf("call with ID %d not found", id))
	}
	return call
}

func (t *Transpiler) input() {
	var err error

	for err == nil {
		// The header is the length in bytes of the remaining message.
		var l uint64
		l, err = binary.ReadUvarint(t.conn)
		if err != nil {
			break
		}

		plen := int(l)
		if len(t.msgBuf) < plen {
			t.msgBuf = make([]byte, plen)
		}

		buf := t.msgBuf[:plen]

		_, err = io.ReadFull(t.conn, buf)
		if err != nil {
			break
		}

		var msg embeddedsass.OutboundMessage

		if err = proto.Unmarshal(buf, &msg); err != nil {
			break
		}

		switch c := msg.Message.(type) {
		case *embeddedsass.OutboundMessage_CompileResponse_:
			id := c.CompileResponse.Id
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
			call := t.getCall(c.CanonicalizeRequest.CompilationId)
			resolved, resolveErr := call.importResolver.CanonicalizeURL(c.CanonicalizeRequest.GetUrl())

			var response *embeddedsass.InboundMessage_CanonicalizeResponse
			if resolveErr != nil {
				response = &embeddedsass.InboundMessage_CanonicalizeResponse{
					Id: c.CanonicalizeRequest.GetId(),
					Result: &embeddedsass.InboundMessage_CanonicalizeResponse_Error{
						Error: resolveErr.Error(),
					},
				}
			} else {
				var url *embeddedsass.InboundMessage_CanonicalizeResponse_Url
				if resolved != "" {
					url = &embeddedsass.InboundMessage_CanonicalizeResponse_Url{
						Url: resolved,
					}
				}
				response = &embeddedsass.InboundMessage_CanonicalizeResponse{
					Id:     c.CanonicalizeRequest.GetId(),
					Result: url,
				}
			}

			err = t.sendInboundMessage(
				&embeddedsass.InboundMessage{
					Message: &embeddedsass.InboundMessage_CanonicalizeResponse_{
						CanonicalizeResponse: response,
					},
				},
			)
		case *embeddedsass.OutboundMessage_ImportRequest_:
			call := t.getCall(c.ImportRequest.CompilationId)
			url := c.ImportRequest.GetUrl()
			contents, loadErr := call.importResolver.Load(url)

			var response *embeddedsass.InboundMessage_ImportResponse
			var sourceMapURL string

			// Dart Sass expect a browser-accessible URL or an empty string.
			// If no URL is supplied, a `data:` URL wil be generated
			// automatically from `contents`
			if hasScheme(url) {
				sourceMapURL = url
			}

			if loadErr != nil {
				response = &embeddedsass.InboundMessage_ImportResponse{
					Id: c.ImportRequest.GetId(),
					Result: &embeddedsass.InboundMessage_ImportResponse_Error{
						Error: loadErr.Error(),
					},
				}
			} else {
				response = &embeddedsass.InboundMessage_ImportResponse{
					Id: c.ImportRequest.GetId(),
					Result: &embeddedsass.InboundMessage_ImportResponse_Success{
						Success: &embeddedsass.InboundMessage_ImportResponse_ImportSuccess{
							Contents:     contents,
							SourceMapUrl: sourceMapURL,
						},
					},
				}
			}

			err = t.sendInboundMessage(
				&embeddedsass.InboundMessage{
					Message: &embeddedsass.InboundMessage_ImportResponse_{
						ImportResponse: response,
					},
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

	// Terminate pending calls.
	t.sendMu.Lock()
	defer t.sendMu.Unlock()
	t.mu.Lock()
	defer t.mu.Unlock()

	t.shutdown = true
	isEOF := err == io.EOF || strings.Contains(err.Error(), "already closed")
	if isEOF {
		if t.closing {
			err = ErrShutdown
		} else {
			err = io.ErrUnexpectedEOF
		}
	}

	for _, call := range t.pending {
		call.Error = err
		call.done()
	}
}

func (t *Transpiler) newCall(createInbound func(seq uint32) (*embeddedsass.InboundMessage, error), args Args) (*call, error) {
	t.mu.Lock()
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

	if t.shutdown || t.closing {
		t.mu.Unlock()
		call.Error = ErrShutdown
		call.done()
		return call, nil
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
	t.mu.Lock()
	if t.closing || t.shutdown {
		t.mu.Unlock()
		return ErrShutdown
	}
	t.mu.Unlock()

	out, err := proto.Marshal(message)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %s", err)
	}

	// Every message must begin with a varint indicating the length in bytes of
	// the remaining message.
	reqLen := uint64(len(out))

	n := binary.PutUvarint(t.lenBuf, reqLen)
	_, err = t.conn.Write(t.lenBuf[:n])
	if err != nil {
		return err
	}

	n, err = t.conn.Write(out)
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

func hasScheme(s string) bool {
	u, err := url.ParseRequestURI(s)
	if err != nil {
		return false
	}
	return u.Scheme != ""
}
