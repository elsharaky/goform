package goform

import (
	"bytes"
	"errors"
	"mime/multipart"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParseKeyPath(t *testing.T) {
	tests := []struct {
		key  string
		base string
		rest []keyToken
	}{
		{"name", "name", nil},
		{"address.city", "address", []keyToken{{"field", "city"}}},
		{"items[0].name", "items", []keyToken{{"index", "0"}, {"field", "name"}}},
		{"attr[key]", "attr", []keyToken{{"mapkey", "key"}}},
		{"matrix[0][1]", "matrix", []keyToken{{"index", "0"}, {"index", "1"}}},
		{"a.b.c", "a", []keyToken{{"field", "b"}, {"field", "c"}}},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			base, rest := parseKeyPath(tt.key)
			if base != tt.base {
				t.Errorf("base = %q, want %q", base, tt.base)
			}
			if len(rest) != len(tt.rest) {
				t.Fatalf("rest = %+v, want %+v", rest, tt.rest)
			}
			for i := range rest {
				if rest[i] != tt.rest[i] {
					t.Errorf("rest[%d] = %+v, want %+v", i, rest[i], tt.rest[i])
				}
			}
		})
	}
}

type decodeBasic struct {
	Name  string `form:"name"`
	Age   int    `form:"age"`
	Email string `json:"user_email"`
}

func TestDecoder_Unmarshal_Basic(t *testing.T) {
	dec := NewDecoder()
	vals := url.Values{
		"name":       {"alice"},
		"age":        {"30"},
		"user_email": {"alice@x.com"},
	}
	var out decodeBasic
	if err := dec.Unmarshal(vals, &out); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if out.Name != "alice" || out.Age != 30 || out.Email != "alice@x.com" {
		t.Errorf("unexpected result: %+v", out)
	}
}

func TestDecoder_Unmarshal_TagPriority(t *testing.T) {
	dec := NewDecoder()
	vals := url.Values{
		"name": {"n"}, // matches form tag (Go name also)
	}
	var out decodeBasic
	if err := dec.Unmarshal(vals, &out); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if out.Name != "n" {
		t.Errorf("Name = %q", out.Name)
	}
}

type decodeNested struct {
	Personal struct {
		Name  string `form:"name"`
		Email string `form:"email"`
	} `form:"personal"`
}

func TestDecoder_Unmarshal_Nested(t *testing.T) {
	dec := NewDecoder()
	vals := url.Values{
		"personal.name":  {"bob"},
		"personal.email": {"bob@x.com"},
	}
	var out decodeNested
	if err := dec.Unmarshal(vals, &out); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if out.Personal.Name != "bob" || out.Personal.Email != "bob@x.com" {
		t.Errorf("unexpected result: %+v", out.Personal)
	}
}

type decodeSlice struct {
	Tags []string `form:"tags"`
}

func TestDecoder_Unmarshal_Slice_Indexed(t *testing.T) {
	dec := NewDecoder()
	vals := url.Values{
		"tags[0]": {"a"},
		"tags[1]": {"b"},
	}
	var out decodeSlice
	if err := dec.Unmarshal(vals, &out); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if !reflect.DeepEqual(out.Tags, []string{"a", "b"}) {
		t.Errorf("Tags = %v", out.Tags)
	}
}

func TestDecoder_Unmarshal_Slice_Repeated(t *testing.T) {
	dec := NewDecoder()
	vals := url.Values{
		"tags": {"a", "b", "c"},
	}
	var out decodeSlice
	if err := dec.Unmarshal(vals, &out); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if !reflect.DeepEqual(out.Tags, []string{"a", "b", "c"}) {
		t.Errorf("Tags = %v", out.Tags)
	}
}

type decodeMap struct {
	Attr map[string]string `form:"attr"`
}

func TestDecoder_Unmarshal_Map(t *testing.T) {
	dec := NewDecoder()
	vals := url.Values{
		"attr[x]": {"1"},
		"attr[y]": {"2"},
	}
	var out decodeMap
	if err := dec.Unmarshal(vals, &out); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if out.Attr["x"] != "1" || out.Attr["y"] != "2" {
		t.Errorf("Attr = %v", out.Attr)
	}
}

type decodeTime struct {
	Created time.Time `form:"created"`
}

func TestDecoder_Unmarshal_Time(t *testing.T) {
	dec := NewDecoder()
	vals := url.Values{"created": {"2024-01-02T15:04:05Z"}}
	var out decodeTime
	if err := dec.Unmarshal(vals, &out); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	expected, _ := time.Parse(time.RFC3339, "2024-01-02T15:04:05Z")
	if !out.Created.Equal(expected) {
		t.Errorf("Created = %v", out.Created)
	}
}

type decodePointers struct {
	Name *string `form:"name"`
}

func TestDecoder_Unmarshal_Pointer(t *testing.T) {
	dec := NewDecoder()
	vals := url.Values{"name": {"ptr"}}
	var out decodePointers
	if err := dec.Unmarshal(vals, &out); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if out.Name == nil || *out.Name != "ptr" {
		t.Errorf("Name = %v", out.Name)
	}
}

func TestDecoder_Unmarshal_Strict_Unknown(t *testing.T) {
	dec := NewDecoder(WithStrictUnmarshal(true))
	vals := url.Values{"unknown": {"x"}}
	var out decodeBasic
	if err := dec.Unmarshal(vals, &out); err == nil {
		t.Fatal("expected error in strict mode for unknown key")
	}
}

func TestDecoder_Unmarshal_Strict_Ignored(t *testing.T) {
	dec := NewDecoder()
	vals := url.Values{"unknown": {"x"}}
	var out decodeBasic
	if err := dec.Unmarshal(vals, &out); err != nil {
		t.Fatalf("unexpected error in non-strict mode: %v", err)
	}
}

// Regression: non-strict mode ignored unknown TOP-LEVEL keys while still
// rejecting unknown paths below a known field ("addr.zip") and structural
// mismatches ("age[0]" on an int, "age.name"). Nested unknowns must be as

func TestDecoder_Unmarshal_NonStrictNestedLenient(t *testing.T) {
	type addr struct {
		City string `form:"city"`
	}
	type s struct {
		Addr addr           `form:"addr"`
		Age  int            `form:"age"`
		Tags []string       `form:"tags"`
		Attr map[string]int `form:"attr"`
	}

	dec := NewDecoder()
	for _, vals := range []url.Values{
		{"addr.zip": {"x"}},
		{"addr.city.zip": {"x"}},
		{"age[0]": {"x"}},
		{"age.name": {"x"}},
		{"attr.status": {"x"}},
	} {
		var out s
		if err := dec.Unmarshal(vals, &out); err != nil {
			t.Errorf("%v: nested unknown should be ignored in non-strict mode, got %v", vals, err)
		}
	}

	// Known nested paths still decode normally beside the ignored noise.
	var out s
	if err := dec.Unmarshal(url.Values{"addr.city": {"lyon"}, "tags[0]": {"a"}, "attr[port]": {"8080"}, "addr.zip": {"x"}}, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Addr.City != "lyon" || out.Tags[0] != "a" || out.Attr["port"] != 8080 {
		t.Errorf("out = %+v", out)
	}
}

func TestDecoder_Unmarshal_StrictNestedRejectsUnknownPath(t *testing.T) {
	type addr struct {
		City string `form:"city"`
	}
	type s struct {
		Addr addr `form:"addr"`
		Age  int  `form:"age"`
	}

	for _, key := range []string{"addr.zip", "addr.city.zip", "age[0]", "age.name"} {
		var out s
		err := NewDecoder(WithStrictUnmarshal(true)).Unmarshal(url.Values{key: {"x"}}, &out)
		var de *DecodingError
		if !errors.As(err, &de) {
			t.Errorf("%s: expected DecodingError in strict mode, got %v", key, err)
			continue
		}
		if de.Err == nil {
			t.Errorf("%s: DecodingError has nil cause", key)
		}
	}

	// A real nested path must still pass strict mode.
	var out s
	if err := NewDecoder(WithStrictUnmarshal(true)).Unmarshal(url.Values{"addr.city": {"lyon"}}, &out); err != nil {
		t.Fatalf("strict unmarshal of valid key: %v", err)
	}
}

func TestDecoder_Unmarshal_NonPointer(t *testing.T) {
	dec := NewDecoder()
	vals := url.Values{}
	var out decodeBasic
	if err := dec.Unmarshal(vals, out); err == nil {
		t.Fatal("expected error for non-pointer")
	}
}

type decodeSliceStruct struct {
	Items []struct {
		Name string `form:"name"`
	} `form:"items"`
}

func TestDecoder_Unmarshal_SliceOfStruct(t *testing.T) {
	dec := NewDecoder()
	vals := url.Values{
		"items[0].name": {"a"},
		"items[1].name": {"b"},
	}
	var out decodeSliceStruct
	if err := dec.Unmarshal(vals, &out); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if len(out.Items) != 2 {
		t.Fatalf("Items len = %d", len(out.Items))
	}
	if out.Items[0].Name != "a" || out.Items[1].Name != "b" {
		t.Errorf("Items = %+v", out.Items)
	}
}

func TestDecoder_Unmarshal_Array(t *testing.T) {
	dec := NewDecoder()
	vals := url.Values{
		"a[0]": {"1"},
		"a[1]": {"2"},
		"a[2]": {"3"},
	}
	var out struct {
		A [3]int `form:"a"`
	}
	if err := dec.Unmarshal(vals, &out); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if out.A != [3]int{1, 2, 3} {
		t.Errorf("A = %v", out.A)
	}
}

func TestDecoder_Unmarshal_MapOfStruct(t *testing.T) {
	dec := NewDecoder()
	vals := url.Values{
		"m[key].name": {"n"},
	}
	var out struct {
		M map[string]struct {
			Name string `form:"name"`
		} `form:"m"`
	}
	if err := dec.Unmarshal(vals, &out); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if out.M["key"].Name != "n" {
		t.Errorf("M = %+v", out.M)
	}
}

// Regression: distinct keys targeting the same map-of-struct element
// ("m[key].name" + "m[key].age", or a struct entry later completed by a file
// part "m[key].bin") must merge into one entry instead of each replacing the

func TestDecoder_Unmarshal_MapOfStructKeysMerge(t *testing.T) {
	dec := NewDecoder()
	vals := url.Values{
		"m[key].name": {"n"},
		"m[key].age":  {"30"},
	}
	var out struct {
		M map[string]struct {
			Name string `form:"name"`
			Age  string `form:"age"`
		} `form:"m"`
	}
	if err := dec.Unmarshal(vals, &out); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if got := out.M["key"]; got.Name != "n" || got.Age != "30" {
		t.Errorf("M[key] = %+v, want {n 30}", got)
	}
}

// Regression: promoted fields from an embedded struct must be addressed by an
// index that is valid on the OUTER struct (embedded field's index prepended),
// not the embedded type's own index. Previously the value was silently dropped

type decodeEmbedCreds struct {
	Token string `form:"token,required"`
}

type decodeEmbedReq struct {
	decodeEmbedCreds
	Note string `form:"note"`
}

func TestDecoder_Unmarshal_EmbeddedPromotedField(t *testing.T) {
	dec := NewDecoder()
	var r decodeEmbedReq
	if err := dec.Unmarshal(url.Values{"token": {"secret-abc"}}, &r); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if r.Token != "secret-abc" {
		t.Errorf("Token = %q, want %q (Note=%q)", r.Token, "secret-abc", r.Note)
	}
	if r.Note != "" {
		t.Errorf("Note = %q, want empty; value leaked to a sibling field", r.Note)
	}
}

func TestDecoder_Unmarshal_EmbeddedPromotedFieldAndSibling(t *testing.T) {
	dec := NewDecoder()
	var r decodeEmbedReq
	if err := dec.Unmarshal(url.Values{"token": {"t"}, "note": {"n"}}, &r); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if r.Token != "t" || r.Note != "n" {
		t.Errorf("got Token=%q Note=%q, want %q %q", r.Token, r.Note, "t", "n")
	}
}

func TestDecoder_Unmarshal_EmbeddedPromotedFieldRequired(t *testing.T) {
	dec := NewDecoder()
	var r decodeEmbedReq
	if err := dec.Unmarshal(url.Values{"note": {"x"}}, &r); err == nil {
		t.Error("expected missing-required error for embedded token")
	}
}

func TestDecoder_Unmarshal_EmbeddedPromotedFieldMultipart(t *testing.T) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if fw, err := mw.CreateFormField("token"); err != nil {
		t.Fatalf("create field: %v", err)
	} else if _, err := fw.Write([]byte("mp-secret")); err != nil {
		t.Fatalf("write field: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	var r decodeEmbedReq
	if err := Unmarshal(buf.Bytes(), mw.FormDataContentType(), &r); err != nil {
		t.Fatalf("unmarshal multipart error: %v", err)
	}
	if r.Token != "mp-secret" {
		t.Errorf("Token = %q, want %q", r.Token, "mp-secret")
	}
}

// Regressions: pointer-embedded structs (*Inner) flatten into the parent
// namespace just like value embeds, mirroring encoding/json. The encoder emits
// the promoted field name, decoding a promoted key allocates the nil embedded
// pointer, the explicit "Inner.sub" key stays valid for older payloads, and

func TestDecoder_Unmarshal_PointerEmbeddedPromotedField(t *testing.T) {
	var r ptrEmbedReq
	if err := NewDecoder().Unmarshal(url.Values{"name": {"n"}}, &r); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if r.PtrEmbedInner == nil {
		t.Fatal("embedded pointer not allocated")
	}
	if r.Name != "n" {
		t.Errorf("Name = %q, want n", r.Name)
	}
	if r.Tag != "" {
		t.Errorf("Tag = %q, want empty", r.Tag)
	}
}

func TestDecoder_Unmarshal_PointerEmbeddedLegacyDottedKey(t *testing.T) {
	var r ptrEmbedReq
	if err := NewDecoder().Unmarshal(url.Values{"PtrEmbedInner.name": {"n"}}, &r); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if r.PtrEmbedInner == nil || r.Name != "n" {
		t.Errorf("embedded pointer = %+v, want non-nil with Name=n", r.PtrEmbedInner)
	}

	// Strict mode accepts the legacy key too (it maps to a real field).
	var strict ptrEmbedReq
	if err := NewDecoder(WithStrictUnmarshal(true)).Unmarshal(url.Values{"PtrEmbedInner.name": {"n"}}, &strict); err != nil {
		t.Fatalf("strict unmarshal of legacy key: %v", err)
	}
}

func TestDecoder_Unmarshal_PointerEmbeddedPromotedFieldRequired(t *testing.T) {
	type ReqInner struct {
		Name string `form:"name,required"`
	}
	type req struct {
		*ReqInner
		Tag string `form:"tag"`
	}
	var r req
	r.ReqInner = &ReqInner{}
	if err := NewDecoder().Unmarshal(url.Values{"tag": {"t"}}, &r); err == nil {
		t.Error("expected missing-required error for promoted field of pointer embed")
	}
}

func TestDecoder_Unmarshal_SliceIndexCapped(t *testing.T) {
	type s struct {
		Items []string `form:"items"`
	}

	dec := NewDecoder()
	var out s
	if err := dec.Unmarshal(url.Values{"items[100001]": {"x"}}, &out); err == nil {
		t.Fatal("expected error for index beyond max slice index")
	}
	if out.Items != nil {
		t.Errorf("Items should not have grown, got len=%d", len(out.Items))
	}

	// Within the cap still works.
	var ok s
	if err := dec.Unmarshal(url.Values{"items[5]": {"x"}}, &ok); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if len(ok.Items) != 6 || ok.Items[5] != "x" {
		t.Errorf("Items = %v", ok.Items)
	}

	// Negative indices are rejected, not a panic.
	var neg s
	if err := dec.Unmarshal(url.Values{"items[-1]": {"x"}}, &neg); err == nil {
		t.Error("expected error for negative index")
	}
}

func TestDecoder_Unmarshal_SliceIndexCustomCap(t *testing.T) {
	type s struct {
		Items []string `form:"items"`
	}
	dec := NewDecoder(WithMaxSliceIndex(10))
	var out s
	if err := dec.Unmarshal(url.Values{"items[10]": {"x"}}, &out); err == nil {
		t.Error("expected error at or above custom cap")
	}
	var ok s
	if err := dec.Unmarshal(url.Values{"items[9]": {"x"}}, &ok); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if len(ok.Items) != 10 {
		t.Errorf("Items len = %d, want 10", len(ok.Items))
	}
}

func TestDecoder_Unmarshal_SliceIndexCapDisabled(t *testing.T) {
	type s struct {
		Items []string `form:"items"`
	}
	dec := NewDecoder(WithMaxSliceIndex(0))
	var out s
	if err := dec.Unmarshal(url.Values{"items[200000]": {"x"}}, &out); err != nil {
		t.Fatalf("unmarshal error with cap disabled: %v", err)
	}
	if len(out.Items) != 200001 {
		t.Errorf("Items len = %d, want 200001", len(out.Items))
	}
}

// Regression: an out-of-range index on a fixed-size array must return an

type decodeFixedArr struct {
	Items [3]string `form:"items"`
}

func TestDecoder_Unmarshal_ArrayOutOfRange(t *testing.T) {
	defer func() {
		if rcv := recover(); rcv != nil {
			t.Fatalf("panicked on out-of-range array index: %v", rcv)
		}
	}()
	dec := NewDecoder()
	var a decodeFixedArr
	if err := dec.Unmarshal(url.Values{"items[10]": {"x"}}, &a); err == nil {
		t.Fatal("expected error for out-of-range array index")
	}
}

func TestDecoder_Unmarshal_ArrayWithinRange(t *testing.T) {
	dec := NewDecoder()
	var a decodeFixedArr
	if err := dec.Unmarshal(url.Values{"items[1]": {"x"}}, &a); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if a.Items[1] != "x" {
		t.Errorf("Items[1] = %q", a.Items[1])
	}
}

// Regression: a tag name shared by an embedded (promoted) field and an outer
// field, or by two sibling fields, previously decoded one field's payload into
// whichever field the index happened to register last — the other field was

type decodeInner struct {
	Name string `form:"name"`
}

type decodeCollideOuter struct {
	decodeInner
	Name string `form:"name"`
}

type decodeCollideSiblings struct {
	A string `form:"name"`
	B string `form:"name"`
}

func TestDecoder_Unmarshal_AmbiguousField(t *testing.T) {
	dec := NewDecoder()
	var out decodeCollideOuter
	err := dec.Unmarshal(url.Values{"name": {"inner-value"}}, &out)
	var de *DecodingError
	if !errors.As(err, &de) {
		t.Fatalf("expected DecodingError, got %v", err)
	}
	if de.Key != "name" {
		t.Errorf("Key = %q, want %q", de.Key, "name")
	}
	// Neither field may be written when the key is ambiguous.
	if out.Name != "" || out.decodeInner.Name != "" {
		t.Errorf("fields mutated despite ambiguous key: %+v", out)
	}
}

func TestDecoder_Unmarshal_AmbiguousFieldSiblings(t *testing.T) {
	dec := NewDecoder(WithStrictUnmarshal(true))
	var out decodeCollideSiblings
	err := dec.Unmarshal(url.Values{"name": {"x"}}, &out)
	var de *DecodingError
	if !errors.As(err, &de) {
		t.Fatalf("expected DecodingError, got %v", err)
	}
	if de.Key != "name" {
		t.Errorf("Key = %q, want %q", de.Key, "name")
	}
	if out.A != "" || out.B != "" {
		t.Errorf("fields mutated despite ambiguous key: %+v", out)
	}
}

func TestDecoder_Unmarshal_AmbiguousNestedField(t *testing.T) {
	type inner struct {
		V string `form:"v"`
	}
	type collide struct {
		X inner  `form:"x"`
		V string `form:"x"` // tag collides with the struct field's key
	}
	var out collide
	err := NewDecoder().Unmarshal(url.Values{"x.v": {"val"}}, &out)
	var de *DecodingError
	if !errors.As(err, &de) {
		t.Fatalf("expected DecodingError, got %v", err)
	}
	if de.Key != "x.v" {
		t.Errorf("Key = %q, want %q", de.Key, "x.v")
	}
	if strings.Contains(de.Err.Error(), "ambiguous") == false {
		t.Errorf("expected ambiguous-cause message, got %v", de.Err)
	}
}

// Regression: a scalar parse failure on a plain field (int overflow, bad bool)
// escaped as a bare error, unlike the same failure reached via a map/slice

func TestDecoder_Unmarshal_ScalarErrorWrapped(t *testing.T) {
	type s struct {
		Int  int   `form:"int"`
		Bool bool  `form:"bool"`
		S    []int `form:"s"`
	}
	dec := NewDecoder()

	for _, key := range []string{"int", "bool", "s[0]"} {
		var out s
		var raw string
		switch key {
		case "int":
			raw = "999999999999999999999"
		case "bool":
			raw = "notabool"
		case "s[0]":
			raw = "999999999999999999999"
		}
		err := dec.Unmarshal(url.Values{key: {raw}}, &out)
		var de *DecodingError
		if !errors.As(err, &de) {
			t.Errorf("%s: expected DecodingError, got %v", key, err)
			continue
		}
		if de.Err == nil {
			t.Errorf("%s: DecodingError has nil cause", key)
		}
	}
}

// Regression: float32 and complex64 previously parsed with 64-bit precision
// and silently accepted out-of-range payloads as +Inf. They now parse with the

func TestDecoder_Unmarshal_FloatOverflowRejected(t *testing.T) {
	type s struct {
		F32 float32   `form:"f32"`
		C64 complex64 `form:"c64"`
	}

	dec := NewDecoder()
	for _, key := range []string{"f32", "c64"} {
		var out s
		raw := "1e300"
		if key == "c64" {
			raw = "1e300+0i"
		}
		err := dec.Unmarshal(url.Values{key: {raw}}, &out)
		var de *DecodingError
		if !errors.As(err, &de) {
			t.Errorf("%s: expected DecodingError for out-of-range value, got %v", key, err)
			continue
		}
	}

	// In-range values still land at full float32/complex64 precision.
	var ok s
	if err := dec.Unmarshal(url.Values{"f32": {"3.25"}, "c64": {"1.5+2i"}}, &ok); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if ok.F32 != 3.25 || ok.C64 != complex(float32(1.5), float32(2)) {
		t.Errorf("got %+v", ok)
	}
}

// Regression: zero-valued time.Time was unconditionally omitted from marshal
// output, unlike every other type (int → "0", string → ""), contradicting the

type mutRefB struct {
	*mutRefA
	BVal string `form:"bval"`
}

type mutRefA struct {
	*mutRefB
	AVal string `form:"aval"`
}

func TestDecoder_Unmarshal_SelfEmbeddedPointer(t *testing.T) {
	type T struct {
		*T
		Name string `form:"name"`
	}
	var out T
	if err := NewDecoder().Unmarshal(url.Values{"name": {"x"}}, &out); err != nil {
		t.Fatalf("unmarshal of self-embedded type: %v", err)
	}
	if out.Name != "x" {
		t.Errorf("Name = %q, want %q", out.Name, "x")
	}
}

func TestDecoder_Unmarshal_MutuallySelfEmbedded(t *testing.T) {
	var out mutRefA
	if err := NewDecoder().Unmarshal(url.Values{"aval": {"a"}}, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.AVal != "a" {
		t.Errorf("AVal = %q, want %q", out.AVal, "a")
	}
}

// #3: a dotted key against a slice used to re-dispatch into the leaf,

func TestDecoder_Unmarshal_DotKeyOnSliceNotInjected(t *testing.T) {
	type s struct {
		Lines []string `form:"lines"`
	}
	var out s
	if err := NewDecoder().Unmarshal(url.Values{"lines.evil": {"injected"}}, &out); err != nil {
		t.Fatalf("lenient unmarshal: %v", err)
	}
	if len(out.Lines) != 0 {
		t.Errorf("Lines = %v, want empty (injection through dotted key)", out.Lines)
	}
	if err := NewDecoder(WithStrictUnmarshal(true)).Unmarshal(url.Values{"lines.evil": {"injected"}}, &out); err == nil {
		t.Error("strict unmarshal: expected error for dotted key on a slice")
	}
}

func TestDecoder_Unmarshal_DotKeyOnArrayNotOverwritten(t *testing.T) {
	type s struct {
		Items [3]int `form:"items"`
	}
	var out s
	if err := NewDecoder().Unmarshal(url.Values{"items.evil": {"99"}}, &out); err != nil {
		t.Fatalf("lenient unmarshal: %v", err)
	}
	if out.Items[0] != 0 {
		t.Errorf("Items[0] = %d, want 0 (overwritten through dotted key)", out.Items[0])
	}
}

func TestDecoder_Unmarshal_DotKeyOnMapRejected(t *testing.T) {
	type s struct {
		Attr map[string]string `form:"attr"`
	}
	var out s
	if err := NewDecoder().Unmarshal(url.Values{"attr.evil": {"x"}}, &out); err != nil {
		t.Fatalf("lenient unmarshal: %v", err)
	}
	if len(out.Attr) != 0 {
		t.Errorf("Attr = %v, want empty", out.Attr)
	}
	if err := NewDecoder(WithStrictUnmarshal(true)).Unmarshal(url.Values{"attr.evil": {"x"}}, &out); err == nil {
		t.Error("strict unmarshal: expected error for dotted key on a map")
	}
}

// #4: integer struct tags forced map[string]int keys; values were previously

func TestDecoder_Unmarshal_NumericKeyIntMap(t *testing.T) {
	type s struct {
		M map[int]string `form:"m"`
	}
	var out s
	if err := NewDecoder().Unmarshal(url.Values{"m[7]": {"x"}}, &out); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if out.M[7] != "x" {
		t.Errorf("M = %v, want map[7:x]", out.M)
	}
}

func TestDecoder_Unmarshal_MapScalarToStructErrors(t *testing.T) {
	type item struct {
		Label string `form:"label"`
	}
	type s struct {
		Items map[string]item `form:"items"`
	}
	var out s
	err := NewDecoder().Unmarshal(url.Values{"items[first]": {"v"}}, &out)
	var de *DecodingError
	if !errors.As(err, &de) {
		t.Fatalf("expected DecodingError, got %v", err)
	}
	if len(out.Items) != 0 {
		t.Errorf("Items = %+v; scalar must not create a zero entry", out.Items)
	}
}

type defaultsReq struct {
	Name   string `form:"name"`
	Region string `form:"region,default:us-east"`
	Count  int    `form:"count,default:5"`
	Token  string `form:"token,required"`
	Flag   bool   `form:"flag,default:true"`
}

func TestDecoder_DefaultTagAppliedWhenMissing(t *testing.T) {
	var r defaultsReq
	vals := url.Values{"name": {"x"}, "token": {"abc"}}
	if err := NewDecoder().Unmarshal(vals, &r); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if r.Region != "us-east" {
		t.Errorf("Region = %q, want us-east", r.Region)
	}
	if r.Count != 5 {
		t.Errorf("Count = %d, want 5", r.Count)
	}
	if !r.Flag {
		t.Errorf("Flag = false, want true")
	}
	if r.Name != "x" || r.Token != "abc" {
		t.Errorf("unexpected: %+v", r)
	}
}

func TestDecoder_DefaultTagPreservesProvidedValue(t *testing.T) {
	var r defaultsReq
	vals := url.Values{"name": {"x"}, "region": {"eu-west"}, "token": {"abc"}}
	if err := NewDecoder().Unmarshal(vals, &r); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if r.Region != "eu-west" {
		t.Errorf("Region = %q, want provided eu-west", r.Region)
	}
}

func TestDecoder_RequiredMissing(t *testing.T) {
	var r defaultsReq
	vals := url.Values{"name": {"x"}} // token missing
	err := NewDecoder().Unmarshal(vals, &r)
	if err == nil {
		t.Fatal("expected error for missing required field")
	}
	if !errors.Is(err, ErrMissingRequired) {
		t.Errorf("expected ErrMissingRequired, got %v", err)
	}
	var de *DecodingError
	if !errors.As(err, &de) {
		t.Fatalf("expected DecodingError, got %T", err)
	}
	if de.Key != "token" {
		t.Errorf("DecodingError.Key = %q, want token", de.Key)
	}
}

func TestDecoder_RequiredProvidedIsOK(t *testing.T) {
	var r defaultsReq
	vals := url.Values{"token": {"abc"}}
	if err := NewDecoder().Unmarshal(vals, &r); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if r.Token != "abc" {
		t.Errorf("Token = %q", r.Token)
	}
}

// Bug 2+3 regressions: required/default tracking must share the decoder's own
// notion of "provided". A field delivered via a nested key ("ship_to.city") or
// an alternate tag name (json:"reg") is provided — it must not raise a
// spurious ErrMissingRequired, and its default must not clobber the value.
type nestedDefaultsReq struct {
	ShipTo struct {
		City string `form:"city,required"`
		Zip  string `form:"zip,default:69001"`
	} `form:"ship_to"`
}

func TestDecoder_RequiredNestedProvidedIsOK(t *testing.T) {
	var r nestedDefaultsReq
	if err := NewDecoder().Unmarshal(url.Values{"ship_to.city": {"Lyon"}}, &r); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if r.ShipTo.City != "Lyon" {
		t.Errorf("City = %q, want Lyon", r.ShipTo.City)
	}
	if r.ShipTo.Zip != "69001" {
		t.Errorf("Zip = %q, want default 69001", r.ShipTo.Zip)
	}
}

func TestDecoder_RequiredNestedMissingStillErrors(t *testing.T) {
	var r nestedDefaultsReq
	err := NewDecoder().Unmarshal(url.Values{"ship_to.zip": {"1000"}}, &r)
	if err == nil {
		t.Fatal("expected error for missing nested required field")
	}
	if !errors.Is(err, ErrMissingRequired) {
		t.Errorf("expected ErrMissingRequired, got %v", err)
	}
}

func TestDecoder_DefaultNestedPreservesProvidedValue(t *testing.T) {
	var r nestedDefaultsReq
	if err := NewDecoder().Unmarshal(url.Values{"ship_to.city": {"Lyon"}, "ship_to.zip": {"1000"}}, &r); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if r.ShipTo.Zip != "1000" {
		t.Errorf("Zip = %q, want provided 1000 (default clobbered it)", r.ShipTo.Zip)
	}
}

func TestDecoder_DefaultNestedAppliedWhenAbsent(t *testing.T) {
	var r nestedDefaultsReq
	if err := NewDecoder().Unmarshal(url.Values{"ship_to.city": {"Lyon"}}, &r); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if r.ShipTo.Zip != "69001" {
		t.Errorf("Zip = %q, want default 69001", r.ShipTo.Zip)
	}
}

type altTagDefaultsReq struct {
	Region string `form:"region,required" json:"reg"`
	Zone   string `form:"zone,default:us" json:"zn"`
}

func TestDecoder_RequiredAltTagProvidedIsOK(t *testing.T) {
	var r altTagDefaultsReq
	if err := NewDecoder().Unmarshal(url.Values{"reg": {"eu"}}, &r); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if r.Region != "eu" {
		t.Errorf("Region = %q, want eu", r.Region)
	}
}

func TestDecoder_DefaultAltTagPreservesProvidedValue(t *testing.T) {
	var r altTagDefaultsReq
	if err := NewDecoder().Unmarshal(url.Values{"reg": {"eu"}, "zn": {"eu-west"}}, &r); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if r.Zone != "eu-west" {
		t.Errorf("Zone = %q, want provided eu-west (default clobbered it)", r.Zone)
	}
}

func TestDecoder_RequiredAltTagMissingStillErrors(t *testing.T) {
	var r altTagDefaultsReq
	err := NewDecoder().Unmarshal(url.Values{"zn": {"eu-west"}}, &r)
	if err == nil {
		t.Fatal("expected error for missing required field")
	}
	if !errors.Is(err, ErrMissingRequired) {
		t.Errorf("expected ErrMissingRequired, got %v", err)
	}
}
