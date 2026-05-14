package router

import (
	"errors"
	"fmt"
	"net/http"
	"reflect"
)

type handlerInfo struct {
	paramType reflect.Type
}

// bindConfig is the per-router state the binder needs at request time:
// the codec for JSON body decode, the cap for multipart and []byte body
// reads. Threaded through register -> buildDispatcher -> buildBinder.
type bindConfig struct {
	codec              Codec
	maxMultipartMemory int64
}

var contextPtrType = reflect.TypeOf((*Context)(nil))

// buildDispatcher reflects on the handler function once at registration and
// returns a cached closure that performs request binding and handler
// invocation for every subsequent request. Handlers write the response
// directly via *Context; they have no return value.
func buildDispatcher(
	handler any,
	cfg bindConfig,
) (http.HandlerFunc, *handlerInfo, error) {
	hv := reflect.ValueOf(handler)
	if !hv.IsValid() {
		return nil, nil, fmt.Errorf("handler is nil")
	}
	ht := hv.Type()
	if ht.Kind() != reflect.Func {
		return nil, nil, fmt.Errorf(
			"handler must be a function, got %T",
			handler,
		)
	}

	if ht.NumIn() < 1 || ht.NumIn() > 2 {
		return nil, nil, fmt.Errorf(
			"handler must take (c *Context) or (c *Context, p Params), got %d args",
			ht.NumIn(),
		)
	}
	if ht.In(0) != contextPtrType {
		return nil, nil, fmt.Errorf(
			"handler first arg must be *router.Context, got %s", ht.In(0),
		)
	}
	if ht.NumOut() != 0 {
		return nil, nil, fmt.Errorf(
			"handler must not return any values, got %d", ht.NumOut(),
		)
	}

	info := &handlerInfo{}
	var binder paramsBinder
	if ht.NumIn() == 2 {
		info.paramType = ht.In(1)
		if info.paramType.Kind() != reflect.Struct {
			return nil, nil, fmt.Errorf(
				"params arg must be a struct, got %s", info.paramType.Kind(),
			)
		}
		b, err := buildBinder(info.paramType, cfg)
		if err != nil {
			return nil, nil, err
		}
		binder = b
	}

	dispatch := func(w http.ResponseWriter, r *http.Request) {
		c := &Context{Writer: w, Request: r, codec: cfg.codec}
		args := []reflect.Value{reflect.ValueOf(c)}
		if binder != nil {
			pv, err := binder(r)
			if err != nil {
				writeBindError(c, err)
				return
			}
			args = append(args, pv)
		}
		hv.Call(args)
	}

	return dispatch, info, nil
}

// writeBindError writes a ProblemDetails for an input-binding failure. The
// binder returns *ProblemDetails directly for path/query/header parse
// errors and bad JSON bodies; this is just the dispatch-time writer.
func writeBindError(c *Context, err error) {
	var pd *ProblemDetails
	if !errors.As(err, &pd) {
		pd = &ProblemDetails{
			Status: http.StatusBadRequest,
			Title:  http.StatusText(http.StatusBadRequest),
			Detail: err.Error(),
		}
	}
	c.Writer.Header().Set("Content-Type", "application/problem+json")
	c.Writer.WriteHeader(pd.Status)
	_ = c.codec.Encode(c.Writer, pd)
}
