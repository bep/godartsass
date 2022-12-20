package functions

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/bep/godartsass/internal/embeddedsass"
)

type functionProxy func([]*embeddedsass.Value) (*embeddedsass.Value, error)

type FunctionRegistry struct {
	functions  map[string]functionProxy
	signatures []string
}

func NewFunctionRegistry(stubs map[string]interface{}) (registry *FunctionRegistry, err error) {
	registry = &FunctionRegistry{
		functions:  make(map[string]functionProxy),
		signatures: []string{},
	}
	if stubs == nil {
		return
	}
	for signature, function := range stubs {
		if err = registry.Register(signature, function); err != nil {
			return
		}
	}
	return
}

func (r *FunctionRegistry) Register(signature string, fn interface{}) (err error) {
	v := reflect.ValueOf(fn)
	t := v.Type()
	if !v.IsValid() || v.Kind() != reflect.Func {
		err = fmt.Errorf("function-registry: invalid function")
		return
	} else if t.NumOut() != 2 || !t.Out(1).Implements(reflect.TypeOf((*error)(nil)).Elem()) {
		err = fmt.Errorf("function-registry: tuple error, expeted returns: (T, error)")
		return
	}
	var name string
	if openParen := strings.IndexRune(signature, '('); openParen == -1 {
		err = fmt.Errorf("%q is missing %q", signature, "(")
		return
	} else {
		name = signature[:openParen]
	}
	r.signatures = append(r.signatures, signature)
	r.functions[name] = func(inputs []*embeddedsass.Value) (output *embeddedsass.Value, err error) {
		if len(inputs) != t.NumIn() {
			err = fmt.Errorf("arguments length error")
			return
		}
		var value reflect.Value
		var inputValues []reflect.Value
		for i := 0; i < t.NumIn(); i++ {
			value, err = UnmarshalValue(inputs[i], t.In(i))
			if err != nil {
				return
			}
			inputValues = append(inputValues, value)
		}
		outputValues := v.Call(inputValues)
		output, err = MarshalValue(outputValues[0])
		if err == nil && !outputValues[1].IsNil() {
			err = outputValues[1].Interface().(error)
		}
		return
	}
	return
}

func (r *FunctionRegistry) Execute(request *embeddedsass.OutboundMessage_FunctionCallRequest) (response *embeddedsass.InboundMessage_FunctionCallResponse) {
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

func (r *FunctionRegistry) SignatureNames() []string {
	var signatures []string
	for _, signature := range r.signatures {
		signatures = append(signatures, strings.Clone(signature))
	}
	return signatures
}
