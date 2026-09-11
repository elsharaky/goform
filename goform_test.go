package goform

import (
	"net/url"
	"testing"
	"time"
)

type unifiedReq struct {
	Name  string            `form:"name"`
	Age   int               `form:"age"`
	When  time.Time         `form:"when"`
	Login *bool             `form:"login"`
	Tags  []string          `form:"tags"`
	Meta  map[string]string `form:"meta"`
}

type unifiedUpload struct {
	Title  string `form:"title"`
	Avatar File   `form:"avatar"`
	Docs   []File `form:"docs"`
}

func TestMarshalURLEncoded(t *testing.T) {
	body, ct, err := Marshal(unifiedReq{Name: "Alice", Age: 30, Tags: []string{"a", "b"}})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if ct != "application/x-www-form-urlencoded" {
		t.Errorf("ct = %q, want urlencoded", ct)
	}
	vals, err := url.ParseQuery(string(body))
	if err != nil {
		t.Fatalf("ParseQuery: %v", err)
	}
	if vals.Get("name") != "Alice" || vals.Get("age") != "30" {
		t.Errorf("unexpected body: %q", body)
	}
}

func TestUnmarshalURLEncoded(t *testing.T) {
	var r unifiedReq
	body := []byte("name=Bob&age=25&tags=a&tags=b")
	if err := Unmarshal(body, "application/x-www-form-urlencoded", &r); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if r.Name != "Bob" || r.Age != 25 {
		t.Errorf("got %+v", r)
	}
	if len(r.Tags) != 2 || r.Tags[0] != "a" || r.Tags[1] != "b" {
		t.Errorf("tags = %v", r.Tags)
	}
}

func TestMarshalAutoDetectMultipart(t *testing.T) {
	_, ct, err := Marshal(unifiedReq{Name: "x"})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if ct != "application/x-www-form-urlencoded" {
		t.Errorf("expected urlencoded, got %q", ct)
	}

	_, ct2, err := Marshal(unifiedUpload{Avatar: File{Content: []byte("x")}})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if ct2 == "application/x-www-form-urlencoded" {
		t.Errorf("expected multipart, got %q", ct2)
	}
}

func TestUnmarshalURLEncodedHTTPCompatible(t *testing.T) {
	var out unifiedReq
	body := []byte("name=carol&age=40")
	if err := Unmarshal(body, "application/x-www-form-urlencoded; charset=UTF-8", &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if out.Name != "carol" || out.Age != 40 {
		t.Errorf("got %+v", out)
	}
}

func TestMarshalTaggedOutFileStaysURLEncoded(t *testing.T) {
	type s struct {
		Name string `form:"name"`
		Doc  File   `form:"-"`
	}
	_, ct, err := Marshal(s{Name: "n"})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if ct != "application/x-www-form-urlencoded" {
		t.Errorf("expected urlencoded despite form:\"-\" File, got %q", ct)
	}
}
