package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/djm56/cairn-memory/internal/core"
)

// keyError is a request key the wire form refuses: one written in the wrong
// case, or one given twice. checkKeys returns it as an invalid core.Error.
type keyError struct{ msg string }

func (e *keyError) Error() string { return e.msg }

// checkKeys walks body, one JSON value, against the Go type v decodes into. It
// refuses a key whose case is not the field's, and a key given twice, at any
// nesting level (CONTRACT §9 item 40). encoding/json would accept both: it
// matches keys without regard to case and keeps the last of a repeated key.
//
// An unknown key is left to the decoder, which refuses it with
// DisallowUnknownFields. A body that is not well formed is left to the decoder
// too, so its message stays the decoder's. A free-form value (a map's values,
// or a json.RawMessage such as event data) has no field names, so only its
// repeated keys are refused.
func checkKeys(body []byte, v any) *core.Error {
	err := walkValue(json.NewDecoder(bytes.NewReader(body)), reflect.TypeOf(v))
	var ke *keyError
	if errors.As(err, &ke) {
		return &core.Error{Kind: core.KindInvalid, Message: ke.msg}
	}
	return nil
}

// walkValue consumes one JSON value from dec. t is the Go type the value
// decodes into, or nil where nothing is known about it. A key error is returned
// as a *keyError; any other error is the decoder's, and stops the walk.
func walkValue(dec *json.Decoder, t reflect.Type) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	switch tok {
	case json.Delim('{'):
		return walkObject(dec, t)
	case json.Delim('['):
		return walkArray(dec, t)
	}
	return nil
}

// walkObject consumes the rest of an object whose '{' has been read. A struct
// names its keys by their JSON names, and a map takes any key with its element
// type as the type of each value.
func walkObject(dec *json.Decoder, t reflect.Type) error {
	t = indirect(t)
	var fields map[string]reflect.Type
	var elem reflect.Type
	if t != nil {
		switch t.Kind() {
		case reflect.Struct:
			fields = map[string]reflect.Type{}
			jsonFields(t, fields)
		case reflect.Map:
			elem = t.Elem()
		}
	}
	seen := map[string]bool{}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		key, _ := tok.(string)
		if seen[key] {
			// The words match core's for a repeated review key, so both paths say the same.
			return &keyError{fmt.Sprintf("the body repeats the key %q: give each key once", key)}
		}
		seen[key] = true
		sub := elem
		if fields != nil {
			ft, exact := fields[key]
			if !exact {
				for name := range fields {
					if strings.EqualFold(name, key) {
						return &keyError{fmt.Sprintf("the key %q must be written %q: keys are case-sensitive", key, name)}
					}
				}
			}
			sub = ft
		}
		if err := walkValue(dec, sub); err != nil {
			return err
		}
	}
	// The closing '}'. The decoder has checked the structure, so this cannot fail.
	_, err := dec.Token()
	return err
}

// walkArray consumes the rest of an array whose '[' has been read. Each element
// takes the element type of t, when t is a slice or an array.
func walkArray(dec *json.Decoder, t reflect.Type) error {
	t = indirect(t)
	var elem reflect.Type
	if t != nil && (t.Kind() == reflect.Slice || t.Kind() == reflect.Array) {
		elem = t.Elem()
	}
	for dec.More() {
		if err := walkValue(dec, elem); err != nil {
			return err
		}
	}
	// The closing ']'. The decoder has checked the structure, so this cannot fail.
	_, err := dec.Token()
	return err
}

// jsonFields adds to into the JSON name of each field of struct type t, and its
// Go type. A field tagged "-" is skipped. An untagged embedded struct is
// flattened, as encoding/json flattens it.
func jsonFields(t reflect.Type, into map[string]reflect.Type) {
	for i := range t.NumField() {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, _, _ := strings.Cut(tag, ",")
		if f.Anonymous && name == "" {
			if ft := indirect(f.Type); ft.Kind() == reflect.Struct {
				jsonFields(ft, into)
				continue
			}
		}
		if !f.IsExported() {
			continue
		}
		if name == "" {
			name = f.Name
		}
		into[name] = f.Type
	}
}

// indirect returns the type a pointer type points to, or t itself. A nil t
// stays nil.
func indirect(t reflect.Type) reflect.Type {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}
