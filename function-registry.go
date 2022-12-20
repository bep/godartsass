package godartsass

import (
	"fmt"
	"strings"

	"github.com/bep/godartsass/internal/embeddedsass"
)

type CustomFunction func([]*embeddedsass.Value) (*embeddedsass.Value, error)

type FunctionRegistry struct {
	functions  map[string]CustomFunction
	signatures []string
}

func NewFunctionRegistry(stubs map[string]CustomFunction) (registry *FunctionRegistry, err error) {
	registry = &FunctionRegistry{
		functions: make(map[string]CustomFunction),
	}
	if stubs == nil {
		return
	}
	for signature, function := range stubs {
		if err = registry.Add(signature, function); err != nil {
			return
		}
	}
	return
}

func (r *FunctionRegistry) Add(signature string, function CustomFunction) (err error) {
	openParen := strings.IndexRune(signature, '(')
	if openParen == -1 {
		err = fmt.Errorf("%q is missing %q", signature, "(")
	}
	name := signature[:openParen]
	r.signatures = append(r.signatures, signature)
	r.functions[name] = function
	return
}

func (r *FunctionRegistry) execute(request *embeddedsass.OutboundMessage_FunctionCallRequest) (response *embeddedsass.InboundMessage_FunctionCallResponse) {
	type Error = embeddedsass.InboundMessage_FunctionCallResponse_Error
	type Success = embeddedsass.InboundMessage_FunctionCallResponse_Success
	response = &embeddedsass.InboundMessage_FunctionCallResponse{Id: request.Id}
	if r == nil {
		response.Result = &Error{Error: "custom-function disabled"}
	} else if callback, ok := r.functions[request.GetName()]; !ok {
		response.Result = &Error{Error: fmt.Sprintf("%q not found", request.GetName())}
	} else if result, err := callback(request.Arguments); err != nil {
		response.Result = &Error{Error: err.Error()}
	} else {
		response.Result = &Success{Success: result}
	}
	return
}
