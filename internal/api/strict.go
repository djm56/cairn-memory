package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/djm56/cairn-memory/internal/core"
)

// maxNestingDepth is the deepest a request body may nest, counting each open
// object or array. It is the limit encoding/json applies (maxNestingDepth in
// encoding/json/scanner.go), and the decoder applies it to the body after the
// walk. The walk recurses once per level, so it must refuse a deeper body itself:
// with no bound, a body of a megabyte of '[' would recurse a million levels, and
// a Go stack overflow is fatal, not a panic that call can recover.
const maxNestingDepth = 10000

// maxEchoedKey is the longest key a refusal message shows. A client can send a
// key as long as the body allows, so a longer key is cut and marked with "…".
const maxEchoedKey = 64

// keyError is a request key the wire form refuses: one written in the wrong
// case, or one given twice. checkKeys returns it as an invalid core.Error.
type keyError struct{ msg string }

func (e *keyError) Error() string { return e.msg }

// depthError is a body nested deeper than maxNestingDepth. checkKeys returns it
// as an invalid core.Error.
type depthError struct{}

func (e *depthError) Error() string {
	return fmt.Sprintf("the body nests deeper than %d levels: reduce the nesting", maxNestingDepth)
}

// checkKeys walks body, one JSON value, against the Go type v decodes into. It
// refuses a key whose case is not the field's, and a key given twice, at any
// nesting level (CONTRACT §9 item 40). encoding/json would accept both: it
// matches keys without regard to case and keeps the last of a repeated key. It
// also refuses a body nested deeper than maxNestingDepth.
//
// An unknown key is left to the decoder, which refuses it with
// DisallowUnknownFields. A body that is not well formed is left to the decoder
// too, so its message stays the decoder's. A free-form value (a map's values,
// or a json.RawMessage such as event data) has no field names, so only its
// repeated keys are refused.
//
// The walk assumes plain encoding/json struct decoding, with no custom
// UnmarshalJSON on a request type. A type with its own unmarshaler would be
// walked as a struct, and the walk could refuse a key that type accepts. No
// request type has one: TestRequestTypesHaveNoUnmarshaler keeps it so. The one
// exception is json.RawMessage, which is free-form by design.
func checkKeys(body []byte, v any) *core.Error {
	err := walkValue(json.NewDecoder(bytes.NewReader(body)), reflect.TypeOf(v), 0)
	var ke *keyError
	if errors.As(err, &ke) {
		return &core.Error{Kind: core.KindInvalid, Message: ke.msg}
	}
	var de *depthError
	if errors.As(err, &de) {
		return &core.Error{Kind: core.KindInvalid, Message: de.Error()}
	}
	return nil
}

// walkValue consumes one JSON value from dec. t is the Go type the value
// decodes into, or nil where nothing is known about it. depth is the number of
// objects and arrays open around the value. A key error or a depth error is
// returned as a *keyError or a *depthError; any other error is the decoder's,
// and stops the walk.
func walkValue(dec *json.Decoder, t reflect.Type, depth int) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	switch tok {
	case json.Delim('{'):
		if depth >= maxNestingDepth {
			return &depthError{}
		}
		return walkObject(dec, t, depth+1)
	case json.Delim('['):
		if depth >= maxNestingDepth {
			return &depthError{}
		}
		return walkArray(dec, t, depth+1)
	}
	return nil
}

// walkObject consumes the rest of an object whose '{' has been read. depth is
// the object's own level, counting it. A struct names its keys by their JSON
// names, and a map takes any key with its element type as the type of each
// value.
func walkObject(dec *json.Decoder, t reflect.Type, depth int) error {
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
			return &keyError{fmt.Sprintf("the body repeats the key %q: give each key once", echoKey(key))}
		}
		seen[key] = true
		sub := elem
		if fields != nil {
			ft, exact := fields[key]
			if !exact {
				for name := range fields {
					if strings.EqualFold(name, key) {
						return &keyError{fmt.Sprintf("the key %q must be written %q: keys are case-sensitive", echoKey(key), name)}
					}
				}
			}
			sub = ft
		}
		if err := walkValue(dec, sub, depth); err != nil {
			return err
		}
	}
	// The closing '}'. The decoder has checked the structure, so this cannot fail.
	_, err := dec.Token()
	return err
}

// walkArray consumes the rest of an array whose '[' has been read. depth is the
// array's own level, counting it. Each element takes the element type of t, when
// t is a slice or an array.
func walkArray(dec *json.Decoder, t reflect.Type, depth int) error {
	t = indirect(t)
	var elem reflect.Type
	if t != nil && (t.Kind() == reflect.Slice || t.Kind() == reflect.Array) {
		elem = t.Elem()
	}
	for dec.More() {
		if err := walkValue(dec, elem, depth); err != nil {
			return err
		}
	}
	// The closing ']'. The decoder has checked the structure, so this cannot fail.
	_, err := dec.Token()
	return err
}

// echoKey returns key as a refusal message shows it: whole when it is at most
// maxEchoedKey bytes, and otherwise cut back to a character boundary and marked
// with "…".
func echoKey(key string) string {
	if len(key) <= maxEchoedKey {
		return key
	}
	cut := maxEchoedKey
	for cut > 0 && !utf8.RuneStart(key[cut]) {
		cut--
	}
	return key[:cut] + "…"
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
