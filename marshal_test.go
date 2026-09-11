package goform

import (
	"net/url"
	"reflect"
	"testing"
	"time"
)

type encodeBasic struct {
	Name  string `form:"name"`
	Age   int    `form:"age"`
	Score float64
	Tags  []string `form:"tags"`
}

func TestEncoder_Marshal_Basic(t *testing.T) {
	enc := NewEncoder()
	vals, err := enc.Marshal(encodeBasic{
		Name:  "alice",
		Age:   30,
		Score: 9.5,
		Tags:  []string{"a", "b"},
	})
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	expected := url.Values{
		"name":    {"alice"},
		"age":     {"30"},
		"Score":   {"9.5"},
		"tags[0]": {"a"},
		"tags[1]": {"b"},
	}
	for k, exp := range expected {
		if got := vals[k]; !reflect.DeepEqual(got, exp) {
			t.Errorf("key %q = %v, want %v", k, got, exp)
		}
	}
}

type encodeNested struct {
	Personal struct {
		Name  string `form:"name"`
		Email string `form:"email"`
	} `form:"personal"`
}

func TestEncoder_Marshal_Nested(t *testing.T) {
	enc := NewEncoder()
	in := encodeNested{}
	in.Personal.Name = "bob"
	in.Personal.Email = "bob@x.com"
	vals, err := enc.Marshal(in)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	if vals.Get("personal.name") != "bob" {
		t.Errorf("personal.name = %q", vals.Get("personal.name"))
	}
	if vals.Get("personal.email") != "bob@x.com" {
		t.Errorf("personal.email = %q", vals.Get("personal.email"))
	}
}

type encodeMaps struct {
	Attr map[string]string `form:"attr"`
}

func TestEncoder_Marshal_Map(t *testing.T) {
	enc := NewEncoder()
	vals, err := enc.Marshal(encodeMaps{Attr: map[string]string{"x": "1", "y": "2"}})
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	if vals.Get("attr[x]") != "1" {
		t.Errorf("attr[x] = %q", vals.Get("attr[x]"))
	}
	if vals.Get("attr[y]") != "2" {
		t.Errorf("attr[y] = %q", vals.Get("attr[y]"))
	}
}

type encodeOmit struct {
	A string `form:"a,omitempty"`
	B string `form:"b"`
}

func TestEncoder_Marshal_OmitEmpty(t *testing.T) {
	enc := NewEncoder()
	vals, err := enc.Marshal(encodeOmit{B: "x"})
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	if _, ok := vals["a"]; ok {
		t.Error("omitempty field should be omitted")
	}
	if vals.Get("b") != "x" {
		t.Errorf("b = %q", vals.Get("b"))
	}
}

type encodeTime struct {
	Created time.Time `form:"created"`
}

func TestEncoder_Marshal_Time(t *testing.T) {
	enc := NewEncoder()
	ts, _ := time.Parse(time.RFC3339, "2024-01-02T15:04:05Z")
	vals, err := enc.Marshal(encodeTime{Created: ts})
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	if vals.Get("created") != "2024-01-02T15:04:05Z" {
		t.Errorf("created = %q", vals.Get("created"))
	}
}

func TestEncoder_Marshal_Layout_Option(t *testing.T) {
	enc := NewEncoder(WithTimeLayout("2006-01-02"))
	ts, _ := time.Parse(time.RFC3339, "2024-01-02T15:04:05Z")
	vals, err := enc.Marshal(encodeTime{Created: ts})
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	if vals.Get("created") != "2024-01-02" {
		t.Errorf("created = %q", vals.Get("created"))
	}
}

type encodeEmbedded struct {
	encodeBasic
	Extra string `form:"extra"`
}

func TestEncoder_Marshal_Embedded(t *testing.T) {
	enc := NewEncoder()
	vals, err := enc.Marshal(encodeEmbedded{encodeBasic{Name: "n", Age: 1}, "e"})
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	if vals.Get("name") != "n" {
		t.Errorf("name = %q", vals.Get("name"))
	}
	if vals.Get("extra") != "e" {
		t.Errorf("extra = %q", vals.Get("extra"))
	}
}

func TestEncoder_Marshal_NonStruct(t *testing.T) {
	enc := NewEncoder()
	_, err := enc.Marshal(42)
	if err == nil {
		t.Fatal("expected error for non-struct")
	}
}

func TestEncoder_Marshal_Pointer(t *testing.T) {
	enc := NewEncoder()
	vals, err := enc.Marshal(&encodeBasic{Name: "p"})
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	if vals.Get("name") != "p" {
		t.Errorf("name = %q", vals.Get("name"))
	}
}

func TestEncoder_Marshal_FileError(t *testing.T) {
	enc := NewEncoder()
	in := struct {
		F File `form:"f"`
	}{}
	_, err := enc.Marshal(in)
	if err == nil {
		t.Fatal("expected file error in url.Values marshalling")
	}
}

type PtrEmbedInner struct {
	Name string `form:"name"`
}

type ptrEmbedReq struct {
	*PtrEmbedInner
	Tag string `form:"tag"`
}

func TestEncoder_Marshal_PointerEmbeddedStructFlattened(t *testing.T) {
	vals, err := NewEncoder().Marshal(ptrEmbedReq{PtrEmbedInner: &PtrEmbedInner{Name: "n"}, Tag: "t"})
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	if vals.Get("name") != "n" {
		t.Errorf("name = %q, want flattened promoted key", vals.Get("name"))
	}
	if vals.Get("tag") != "t" {
		t.Errorf("tag = %q", vals.Get("tag"))
	}
	if vals.Has("PtrEmbedInner.name") {
		t.Errorf("unexpected dotted ptr-embed key: %v", vals)
	}

	// A nil embedded pointer contributes nothing (and does not error).
	vals, err = NewEncoder().Marshal(ptrEmbedReq{Tag: "t"})
	if err != nil {
		t.Fatalf("marshal nil-embed error: %v", err)
	}
	if vals.Has("name") || vals.Get("tag") != "t" {
		t.Errorf("nil-embed output = %v", vals)
	}
}

func TestEncoder_Marshal_ZeroTimeEmitted(t *testing.T) {
	type s struct {
		When time.Time `form:"when"`
	}
	vals, err := NewEncoder().Marshal(s{})
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	if vals.Get("when") != "0001-01-01T00:00:00Z" {
		t.Errorf("when = %q, want %q", vals.Get("when"), "0001-01-01T00:00:00Z")
	}
}

func TestEncoder_Marshal_ZeroTimeOmittedWithOmitEmpty(t *testing.T) {
	type s struct {
		When time.Time `form:"when"`
	}
	vals, err := NewEncoder(WithZeroEmpty(true)).Marshal(s{})
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	if vals.Has("when") {
		t.Errorf("zero time should be omitted under WithZeroEmpty, got %v", vals)
	}
}

// === Round-3 review regressions ===

// roundTxtCode is a string-kind type implementing TextMarshaler/TextUnmarshaler.
// Before the round-3 fix the encoder skipped string-kind TextMarshalers, so the
// encoded value leaked raw while decoding still dispatched to UnmarshalText
