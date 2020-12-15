package godartsass

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"sync"

	"github.com/bep/godartsass/internal/embeddedsass"
	"google.golang.org/protobuf/proto"
)

var (
	defaultDartSassEmbeddedFilename = "dart-sass-embedded"
)

const (
	dummyImportSchema = "godartimport:"
)

func init() {
	if isWindows() {
		defaultDartSassEmbeddedFilename += ".bat"
	}
}

// Start creates an starts a new SCSS transpiler that communicates with the
// Dass Sass Embedded protocol via Stdin and Stdout.
//
// Closing the transpiler will shut down the process.
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

type Result struct {
	CSS string
}

// ImportResolver allows custom import resolution.
// CanonicalizeURL should create a canonical version of the given URL if it's
// able to resolve it, else return an empty string.
// Include scheme if relevant, e.g. 'file://foo/bar.scss'.
// Importers   must ensure that the same canonical URL
// always refers to the same stylesheet.
//
// Load loads the canonicalized URL's content.
// TODO1 consider errors.
type ImportResolver interface {
	CanonicalizeURL(url string) string
	Load(canonicalizedURL string) string
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

const importerID = 5679

func (c *Transpiler) Execute(args Args) (Result, error) {
	var result Result

	if err := args.init(); err != nil {
		return result, err
	}

	message := &embeddedsass.InboundMessage_CompileRequest_{
		CompileRequest: &embeddedsass.InboundMessage_CompileRequest{
			Importers: c.opts.createImporters(),
			Style:     args.sassOutputStyle,
			Input: &embeddedsass.InboundMessage_CompileRequest_String_{
				String_: &embeddedsass.InboundMessage_CompileRequest_StringInput{
					Syntax: args.sassSourceSyntax,
					Source: args.Source,
				},
			},
		},
	}

	resp, err := c.invoke(
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
	case *embeddedsass.OutboundMessage_CompileResponse_Failure:
		// TODO1 create a better error: offset, context etc.
		return result, fmt.Errorf("compile failed: %s", resp.Failure.GetMessage())
	default:
		return result, fmt.Errorf("unsupported response type: %T", resp)
	}

	return result, nil
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

type debugReadWriteCloser struct {
	io.ReadWriteCloser
}

func (w debugReadWriteCloser) Read(p []byte) (n int, err error) {
	n, err = w.ReadWriteCloser.Read(p)
	fmt.Printf("READ=>%q<=\n", p)
	return
}

func (w debugReadWriteCloser) Write(p []byte) (n int, err error) {
	n, err = w.ReadWriteCloser.Write(p)
	fmt.Printf("Write=>%q<=\n", p)
	return
}

func isWindows() bool {
	return runtime.GOOS == "windows"
}
