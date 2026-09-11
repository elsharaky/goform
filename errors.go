package goform

import (
	"errors"
	"fmt"
	"strings"
)

// Sentinel errors returned by goform operations.
var (
	// ErrNotStruct is returned when Marshal or Unmarshal receives a non-struct type.
	ErrNotStruct = errors.New("goform: expected struct type")

	// ErrNilPointer is passed a nil pointer to Marshal or Unmarshal.
	ErrNilPointer = errors.New("goform: nil pointer")

	// ErrMissingRequired is returned when a required field is absent during Unmarshal.
	ErrMissingRequired = errors.New("goform: missing required field")

	// ErrFileNotSupported is returned when a File field is encountered during
	// URL-value marshalling. Use MarshalMultipart instead.
	ErrFileNotSupported = errors.New("goform: File fields require multipart encoding; use MarshalMultipart")

	// ErrMaxDepthExceeded is returned when nested struct depth exceeds the configured limit.
	ErrMaxDepthExceeded = errors.New("goform: max nesting depth exceeded")

	// ErrBodyTooLarge is returned when a body exceeds the limit set by WithMaxBodySize.
	ErrBodyTooLarge = errors.New("goform: body exceeds maximum size")

	// ErrFileTooLarge is returned when a file part exceeds the limit set by WithMaxFileSize.
	ErrFileTooLarge = errors.New("goform: file exceeds maximum size")
)

// EncodingError wraps a field path and underlying cause during marshalling.
type EncodingError struct {
	FieldPath string
	Err       error
}

func (e *EncodingError) Error() string {
	if e.FieldPath != "" {
		return fmt.Sprintf("goform: encoding field %s: %v", e.FieldPath, e.Err)
	}
	return fmt.Sprintf("goform: encoding: %v", e.Err)
}

func (e *EncodingError) Unwrap() error { return e.Err }

// DecodingError wraps a field path, key, and underlying cause during unmarshalling.
type DecodingError struct {
	FieldPath string
	Key       string
	Err       error
}

func (e *DecodingError) Error() string {
	var b strings.Builder
	b.WriteString("goform: decoding")
	if e.FieldPath != "" {
		fmt.Fprintf(&b, " field %s", e.FieldPath)
	}
	if e.Key != "" {
		fmt.Fprintf(&b, " (key %q)", e.Key)
	}
	fmt.Fprintf(&b, ": %v", e.Err)
	return b.String()
}

func (e *DecodingError) Unwrap() error { return e.Err }
