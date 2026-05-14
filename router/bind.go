package router

import (
	"fmt"
	"net/http"
	"reflect"
	"strconv"
)

// paramsBinder reads HTTP request data and populates a Params struct.
type paramsBinder func(*http.Request) (reflect.Value, error)

type fieldBinder struct {
	index  []int
	source string // path, query, header, body
	key    string
}

// buildBinder reflects on a Params struct type once at registration and
// returns a closure that populates a fresh instance from a request.
func buildBinder(t reflect.Type, codec Codec) (paramsBinder, error) {
	binders := collectBinders(t, nil)

	return func(r *http.Request) (reflect.Value, error) {
		v := reflect.New(t).Elem()
		for _, fb := range binders {
			field := v.FieldByIndex(fb.index)
			switch fb.source {
			case "path":
				if err := setScalar(field, r.PathValue(fb.key)); err != nil {
					return reflect.Value{}, BadRequest(fmt.Sprintf(
						"path parameter %q: %v", fb.key, err,
					))
				}
			case "query":
				raw := r.URL.Query().Get(fb.key)
				if raw == "" {
					continue
				}
				if err := setScalar(field, raw); err != nil {
					return reflect.Value{}, BadRequest(fmt.Sprintf(
						"query parameter %q: %v", fb.key, err,
					))
				}
			case "header":
				raw := r.Header.Get(fb.key)
				if raw == "" {
					continue
				}
				if err := setScalar(field, raw); err != nil {
					return reflect.Value{}, BadRequest(fmt.Sprintf(
						"header %q: %v", fb.key, err,
					))
				}
			case "body":
				if err := codec.Decode(r.Body, field.Addr().Interface()); err != nil {
					return reflect.Value{}, BadRequest("body: " + err.Error())
				}
			}
		}
		return v, nil
	}, nil
}

func collectBinders(t reflect.Type, parent []int) []fieldBinder {
	var out []fieldBinder
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		idx := append(append([]int(nil), parent...), i)
		for _, source := range []string{"path", "query", "header", "body"} {
			if key, ok := f.Tag.Lookup(source); ok {
				out = append(out, fieldBinder{
					index:  idx,
					source: source,
					key:    key,
				})
				break
			}
		}
	}
	return out
}

func setScalar(v reflect.Value, raw string) error {
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			v.Set(reflect.New(v.Type().Elem()))
		}
		return setScalar(v.Elem(), raw)
	}
	if raw == "" && v.Kind() != reflect.String {
		return nil
	}
	switch v.Kind() {
	case reflect.String:
		v.SetString(raw)
	case reflect.Bool:
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return err
		}
		v.SetBool(b)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		bits := v.Type().Bits()
		n, err := strconv.ParseInt(raw, 10, bits)
		if err != nil {
			return err
		}
		v.SetInt(n)
	case reflect.Uint,
		reflect.Uint8,
		reflect.Uint16,
		reflect.Uint32,
		reflect.Uint64:
		bits := v.Type().Bits()
		n, err := strconv.ParseUint(raw, 10, bits)
		if err != nil {
			return err
		}
		v.SetUint(n)
	case reflect.Float32, reflect.Float64:
		bits := v.Type().Bits()
		f, err := strconv.ParseFloat(raw, bits)
		if err != nil {
			return err
		}
		v.SetFloat(f)
	default:
		return fmt.Errorf("unsupported field type %s", v.Type())
	}
	return nil
}
