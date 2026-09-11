package goform

import (
	"bytes"
	"fmt"
	"net/url"
	"strings"
	"testing"
)

func TestMarshalUnmarshal_RoundTrip_PointerEmbeddedStruct(t *testing.T) {
	var req struct {
		*PtrEmbedInner
		Tag string `form:"tag"`
	}
	req.PtrEmbedInner = &PtrEmbedInner{Name: "round"}
	req.Tag = "t"

	vals, err := NewEncoder().Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got struct {
		*PtrEmbedInner
		Tag string `form:"tag"`
	}
	if err := NewDecoder().Unmarshal(vals, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Name != "round" || got.Tag != "t" {
		t.Errorf("round trip = %+v", got)
	}
}

type roundTxtCode string

func (c roundTxtCode) MarshalText() ([]byte, error) {
	return []byte("code:" + string(c)), nil
}

func (c *roundTxtCode) UnmarshalText(b []byte) error {
	*c = roundTxtCode(strings.TrimPrefix(string(b), "code:"))
	return nil
}

func TestRoundTrip_StringKindTextMarshalerSymmetric(t *testing.T) {
	type s struct {
		Code roundTxtCode `form:"code"`
	}
	in := s{Code: roundTxtCode("abc")}
	vals, err := NewEncoder().Marshal(in)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	if got := vals.Get("code"); got != "code:abc" {
		t.Errorf("marshal = %q, want TextMarshaler output %q", got, "code:abc")
	}
	var out s
	if err := NewDecoder().Unmarshal(vals, &out); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if out.Code != "abc" {
		t.Errorf("Code = %q, want %q", out.Code, "abc")
	}
}

// txtPoint is a struct-kind type implementing TextMarshaler/TextUnmarshaler.
// The encoder emits "X,Y" but the decoder used to ignore it because a struct

type txtPoint struct {
	X, Y int
}

func (p txtPoint) MarshalText() ([]byte, error) {
	return []byte(fmt.Sprintf("%d,%d", p.X, p.Y)), nil
}

func (p *txtPoint) UnmarshalText(b []byte) error {
	_, err := fmt.Sscanf(string(b), "%d,%d", &p.X, &p.Y)
	return err
}

// mutRefA and mutRefB are mutually-recursive pointer types used to reproduce

func TestRoundTrip_StructKindTextMarshalerSymmetric(t *testing.T) {
	type s struct {
		Loc txtPoint `form:"loc"`
	}
	in := s{Loc: txtPoint{X: 3, Y: 4}}
	vals, err := NewEncoder().Marshal(in)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	if got := vals.Get("loc"); got != "3,4" {
		t.Errorf("marshal = %q, want %q", got, "3,4")
	}
	var out s
	if err := NewDecoder().Unmarshal(vals, &out); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if out.Loc != (txtPoint{X: 3, Y: 4}) {
		t.Errorf("Loc = %+v, want {3 4}", out.Loc)
	}
}

func TestRoundTrip_MultipartTextMarshalerRoundTrip(t *testing.T) {
	enc := NewEncoder()
	body, ct, err := enc.MarshalMultipart(struct {
		Code roundTxtCode `form:"code"`
		Loc  txtPoint     `form:"loc"`
	}{Code: "abc", Loc: txtPoint{X: 5, Y: 6}})
	if err != nil {
		t.Fatalf("marshal multipart error: %v", err)
	}
	if !bytes.Contains(body, []byte("code:abc")) || !bytes.Contains(body, []byte("5,6")) {
		t.Errorf("multipart body missing TextMarshaler output:\n%s", body)
	}
	var out struct {
		Code roundTxtCode `form:"code"`
		Loc  txtPoint     `form:"loc"`
	}
	if err := Unmarshal(body, ct, &out); err != nil {
		t.Fatalf("unmarshal multipart error: %v", err)
	}
	if out.Code != "abc" || out.Loc != (txtPoint{X: 5, Y: 6}) {
		t.Errorf("round trip = %+v/%+v", out.Code, out.Loc)
	}
}

func TestRoundTrip_NumericKeyMapRoundTrip(t *testing.T) {
	type s struct {
		Nums map[string]int `form:"nums"`
	}
	in := s{Nums: map[string]int{"123": 5, "-1": 6}}
	vals, err := NewEncoder().Marshal(in)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	var out s
	if err := NewDecoder().Unmarshal(vals, &out); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if len(out.Nums) != 2 || out.Nums["123"] != 5 || out.Nums["-1"] != 6 {
		t.Errorf("Nums = %v, want {123:5 -1:6}", out.Nums)
	}
}

func TestRoundTrip_BytesSingleValue(t *testing.T) {
	type s struct {
		Data []byte `form:"data"`
	}
	in := s{Data: []byte("hello\x00world")}
	vals, err := NewEncoder().Marshal(in)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	if got := vals.Get("data"); got != "hello\x00world" {
		t.Errorf("marshal = %q, want single raw value", got)
	}
	var out s
	if err := NewDecoder().Unmarshal(vals, &out); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if !bytes.Equal(out.Data, in.Data) {
		t.Errorf("round trip = %q, want %q", out.Data, in.Data)
	}

	var single s
	if err := NewDecoder().Unmarshal(url.Values{"data": {"hi"}}, &single); err != nil {
		t.Fatalf("single-value unmarshal: %v", err)
	}
	if string(single.Data) != "hi" {
		t.Errorf("Data = %q, want %q", single.Data, "hi")
	}

	// indexed numeric form must keep working
	var idx s
	if err := NewDecoder().Unmarshal(url.Values{"data[0]": {"72"}}, &idx); err != nil {
		t.Fatalf("indexed unmarshal: %v", err)
	}
	if string(idx.Data) != "H" {
		t.Errorf("Data = %q, want %q", idx.Data, "H")
	}
}

func TestRoundTrip_MultipartRoundTrip(t *testing.T) {
	in := unifiedUpload{
		Title:  "Hello",
		Avatar: File{Filename: "a.png", ContentType: "image/png", Content: []byte("PNG")},
		Docs: []File{
			{Filename: "one.txt", ContentType: "text/plain", Content: []byte("one")},
			{Filename: "two.txt", ContentType: "text/plain", Content: []byte("two")},
		},
	}

	body, ct, err := Marshal(in)
	if err != nil {
		t.Fatalf("Marshal multipart: %v", err)
	}
	if ct == "application/x-www-form-urlencoded" {
		t.Fatal("expected multipart content type")
	}

	var out unifiedUpload
	if err := Unmarshal(body, ct, &out); err != nil {
		t.Fatalf("Unmarshal multipart: %v", err)
	}

	if out.Title != "Hello" {
		t.Errorf("title = %q", out.Title)
	}
	if string(out.Avatar.Content) != "PNG" || out.Avatar.Filename != "a.png" {
		t.Errorf("avatar = %+v", out.Avatar)
	}
	if len(out.Docs) != 2 || string(out.Docs[1].Content) != "two" {
		t.Errorf("docs = %+v", out.Docs)
	}
}
