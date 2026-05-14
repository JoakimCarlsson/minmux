package router

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
)

type handlerInfo struct {
	paramType  reflect.Type
	resultType reflect.Type
}

var (
	contextType = reflect.TypeOf((*context.Context)(nil)).Elem()
	errorType   = reflect.TypeOf((*error)(nil)).Elem()
)

// buildDispatcher reflects on the handler function once at registration and
// returns a cached closure that performs request binding, handler invocation,
// and response writing for every subsequent request.
func buildDispatcher(
	handler any,
	codec Codec,
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
			"handler must take (ctx) or (ctx, params), got %d args", ht.NumIn(),
		)
	}
	if ht.In(0) != contextType {
		return nil, nil, fmt.Errorf(
			"handler first arg must be context.Context, got %s", ht.In(0),
		)
	}
	if ht.NumOut() != 2 {
		return nil, nil, fmt.Errorf(
			"handler must return (T, error), got %d return values", ht.NumOut(),
		)
	}
	if ht.Out(1) != errorType {
		return nil, nil, fmt.Errorf(
			"handler second return must be `error`, got %s", ht.Out(1),
		)
	}

	info := &handlerInfo{resultType: ht.Out(0)}

	var binder paramsBinder
	if ht.NumIn() == 2 {
		info.paramType = ht.In(1)
		if info.paramType.Kind() != reflect.Struct {
			return nil, nil, fmt.Errorf(
				"params arg must be a struct, got %s", info.paramType.Kind(),
			)
		}
		b, err := buildBinder(info.paramType, codec)
		if err != nil {
			return nil, nil, err
		}
		binder = b
	}

	dispatch := func(w http.ResponseWriter, r *http.Request) {
		args := []reflect.Value{reflect.ValueOf(r.Context())}
		if binder != nil {
			pv, err := binder(r)
			if err != nil {
				writeError(w, codec, err)
				return
			}
			args = append(args, pv)
		}

		out := hv.Call(args)
		if !out[1].IsNil() {
			writeError(w, codec, out[1].Interface().(error))
			return
		}
		writeResult(w, codec, out[0])
	}

	return dispatch, info, nil
}

func writeResult(w http.ResponseWriter, c Codec, result reflect.Value) {
	iface := result.Interface()
	if resp, ok := iface.(Response); ok {
		_ = resp.WriteResponse(w, c)
		return
	}
	w.Header().Set("Content-Type", c.ContentType())
	w.WriteHeader(http.StatusOK)
	_ = c.Encode(w, iface)
}

func writeError(w http.ResponseWriter, c Codec, err error) {
	var pd *ProblemDetails
	if errors.As(err, &pd) {
		writeProblem(w, c, pd)
		return
	}
	var se StatusError
	if errors.As(err, &se) {
		writeProblem(w, c, &ProblemDetails{
			Status: se.HTTPStatus(),
			Title:  http.StatusText(se.HTTPStatus()),
			Detail: err.Error(),
		})
		return
	}
	writeProblem(w, c, &ProblemDetails{
		Status: http.StatusInternalServerError,
		Title:  http.StatusText(http.StatusInternalServerError),
		Detail: err.Error(),
	})
}

func writeProblem(w http.ResponseWriter, c Codec, pd *ProblemDetails) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(pd.Status)
	_ = c.Encode(w, pd)
}
