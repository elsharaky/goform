package goform

import (
	"bytes"
	"errors"
	"mime/multipart"
	"net/http/httptest"
	"strings"
	"testing"
)

func buildAliasBody(t *testing.T, field, content string) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile(field, "x.txt")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := fw.Write([]byte(content)); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	return buf.Bytes(), mw.FormDataContentType()
}

// buildAliasValueBody creates a multipart body with a VALUE part (no filename).
// The multipart parser routes such parts to mf.Value, never mf.File.
func buildAliasValueBody(t *testing.T, field, content string) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormField(field)
	if err != nil {
		t.Fatalf("create form field: %v", err)
	}
	if _, err := fw.Write([]byte(content)); err != nil {
		t.Fatalf("write field: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	return buf.Bytes(), mw.FormDataContentType()
}

// A value part applied to a File field is the signature of an untouched
// browser file input: its empty filename makes the multipart parser route it
// to the value store. Failing the whole decode for an optional file box the
// user left empty would break a normal form, so the part is skipped and the
// rest of the request still decodes (mirroring the encoder, which omits Files
// without a filename).
func TestFileFieldValuePartSkipped(t *testing.T) {
	type S struct {
		Avatar File `form:"avatar"`
	}

	body, ct := buildAliasValueBody(t, "avatar", "plain")
	var v S
	if err := Unmarshal(body, ct, &v); err != nil {
		t.Fatalf("untouched file input must not fail the decode: %v", err)
	}
	if v.Avatar.Filename != "" || len(v.Avatar.Content) != 0 {
		t.Fatalf("avatar should have been skipped, got %+v", v.Avatar)
	}
}

func TestFileSliceFieldValuePartSkipped(t *testing.T) {
	type S struct {
		Docs []File `form:"docs"`
	}

	body, ct := buildAliasValueBody(t, "docs", "plain")
	var v S
	if err := Unmarshal(body, ct, &v); err != nil {
		t.Fatalf("untouched file input must not fail the decode: %v", err)
	}
	if len(v.Docs) != 0 {
		t.Fatalf("docs should have been skipped, got %+v", v.Docs)
	}
}

func TestFilePtrFieldValuePartSkipped(t *testing.T) {
	type S struct {
		Avatar *File `form:"avatar"`
	}

	body, ct := buildAliasValueBody(t, "avatar", "plain")
	var v S
	if err := Unmarshal(body, ct, &v); err != nil {
		t.Fatalf("untouched file input must not fail the decode: %v", err)
	}
	if v.Avatar != nil {
		t.Fatalf("avatar should have been skipped, got %+v", v.Avatar)
	}
}

// Bug #1: file fields must accept any tag key (form, json, xml, protobuf, Go name),
// matching how value fields behave.
func TestFile_FieldAcceptsJSONTagKey(t *testing.T) {
	type S struct {
		Avatar File `json:"avatar" form:"avatar_file"`
	}

	for name, part := range map[string]string{
		"json key":    "avatar",
		"form key":    "avatar_file",
		"go name key": "Avatar",
	} {
		t.Run(name, func(t *testing.T) {
			body, ct := buildAliasBody(t, part, "hello "+part)
			var v S
			if err := Unmarshal(body, ct, &v); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if string(v.Avatar.Content) != "hello "+part {
				t.Fatalf("file content = %q, want %q", v.Avatar.Content, "hello "+part)
			}
		})
	}
}

func TestFile_SliceAcceptsJSONTagKey(t *testing.T) {
	type S struct {
		Docs []File `json:"docs" form:"doc_files"`
	}

	body, ct := buildAliasBody(t, "docs", "multi-content")
	var v S
	if err := Unmarshal(body, ct, &v); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(v.Docs) != 1 || string(v.Docs[0].Content) != "multi-content" {
		t.Fatalf("docs = %+v", v.Docs)
	}
}

func TestFile_PtrAcceptsJSONTagKey(t *testing.T) {
	type S struct {
		Avatar *File `json:"avatar" form:"avatar_file"`
	}

	body, ct := buildAliasBody(t, "avatar", "ptr-content")
	var v S
	if err := Unmarshal(body, ct, &v); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if v.Avatar == nil || string(v.Avatar.Content) != "ptr-content" {
		t.Fatalf("avatar = %+v", v.Avatar)
	}
}

// Bug #2: WithStrictUnmarshal must reject unknown multipart file parts,
// matching the value-field behavior.
func TestFile_StrictUnmarshalRejectsUnknownPart(t *testing.T) {
	type S struct {
		Doc File `form:"doc"`
	}

	body, ct := buildAliasBody(t, "unknown", "surprise")
	var v S
	err := Unmarshal(body, ct, &v, WithStrictUnmarshal(true))
	if err == nil {
		t.Fatal("expected error for unknown file part")
	}
	var de *DecodingError
	if !errors.As(err, &de) || de.Key != "unknown" {
		t.Fatalf("expected DecodingError with Key=unknown, got %v", err)
	}
}

func TestFile_StrictUnmarshalAcceptsKnownPart(t *testing.T) {
	type S struct {
		Doc File `form:"doc"`
	}

	body, ct := buildAliasBody(t, "doc", "known-content")
	var v S
	if err := Unmarshal(body, ct, &v, WithStrictUnmarshal(true)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(v.Doc.Content) != "known-content" {
		t.Fatalf("doc = %q", v.Doc.Content)
	}
}

func TestFile_StrictUnmarshalAcceptsKnownAlias(t *testing.T) {
	type S struct {
		Doc File `json:"docx" form:"doc"`
	}

	body, ct := buildAliasBody(t, "docx", "alias-content")
	var v S
	if err := Unmarshal(body, ct, &v, WithStrictUnmarshal(true)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(v.Doc.Content) != "alias-content" {
		t.Fatalf("doc = %q", v.Doc.Content)
	}
}

// Strict enforcement must also work via the Decoder/UnmarshalMultipartForm path.
func TestFile_StrictUnknownPartViaDecoder(t *testing.T) {
	type S struct {
		Doc File `form:"doc"`
	}

	body, ct := buildAliasBody(t, "unknown", "surprise")
	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req.Header.Set("Content-Type", ct)
	if err := req.ParseMultipartForm(1 << 20); err != nil {
		t.Fatalf("parse multipart: %v", err)
	}

	dec := NewDecoder(WithStrictUnmarshal(true))
	var v S
	err := dec.UnmarshalMultipartForm(req.MultipartForm, &v)
	if err == nil {
		t.Fatal("expected error for unknown file part")
	}
	var de *DecodingError
	if !errors.As(err, &de) || de.Key != "unknown" {
		t.Fatalf("expected DecodingError with Key=unknown, got %v", err)
	}
}

// Embedded structs can carry both value and file fields; the multipart file
// path recurses into the embedded struct directly and must land on the outer
// File field.
type embedFileMeta struct {
	Title string `form:"title"`
}

type embedUploadReq struct {
	embedFileMeta
	Doc File `form:"doc"`
}

func TestFile_EmbeddedStructFileMultipart(t *testing.T) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if fw, err := mw.CreateFormField("title"); err != nil {
		t.Fatalf("create field: %v", err)
	} else if _, err := fw.Write([]byte("t1")); err != nil {
		t.Fatalf("write field: %v", err)
	}
	if pw, err := mw.CreateFormFile("doc", "report.pdf"); err != nil {
		t.Fatalf("create file: %v", err)
	} else if _, err := pw.Write([]byte("%PDF-1.5")); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	var r embedUploadReq
	if err := Unmarshal(buf.Bytes(), mw.FormDataContentType(), &r); err != nil {
		t.Fatalf("unmarshal multipart error: %v", err)
	}
	if r.Title != "t1" {
		t.Errorf("Title = %q, want %q", r.Title, "t1")
	}
	if r.Doc.Filename != "report.pdf" || string(r.Doc.Content) != "%PDF-1.5" {
		t.Errorf("Doc = %+v, want filename report.pdf", r.Doc)
	}
}

// Regression: two File fields sharing a tag name (siblings, or an embedded
// promoted File plus an outer File) must not both consume the same file part.
// Previously each matching field received the part; now the part is rejected
// as ambiguous, mirroring the value-path behavior.
type embedFileCollide struct {
	Doc File `form:"doc"`
}

type uploadFileCollide struct {
	embedFileCollide
	Doc File `form:"doc"`
}

func TestFile_AmbiguousPartSiblings(t *testing.T) {
	type S struct {
		Doc  File `form:"doc"`
		Blob File `form:"doc"`
	}

	body, ct := buildAliasBody(t, "doc", "dup")
	var v S
	err := Unmarshal(body, ct, &v)
	var de *DecodingError
	if !errors.As(err, &de) {
		t.Fatalf("expected DecodingError, got %v", err)
	}
	if de.Key != "doc" {
		t.Errorf("Key = %q, want %q", de.Key, "doc")
	}
	if v.Doc.Content != nil || v.Blob.Content != nil {
		t.Errorf("fields consumed despite ambiguous part: %+v", v)
	}
}

func TestFile_AmbiguousPartEmbedded(t *testing.T) {
	body, ct := buildAliasBody(t, "doc", "dup")
	var v uploadFileCollide
	err := Unmarshal(body, ct, &v)
	var de *DecodingError
	if !errors.As(err, &de) {
		t.Fatalf("expected DecodingError, got %v", err)
	}
	if de.Key != "doc" {
		t.Errorf("Key = %q, want %q", de.Key, "doc")
	}
	if v.Doc.Content != nil || v.embedFileCollide.Doc.Content != nil {
		t.Errorf("fields consumed despite ambiguous part: %+v", v)
	}
}

// Bug 1 regressions: File fields inside NAMED nested structs must round-trip.
// The encoder emits dotted part names ("meta.avatar"); the decoder routes file
// parts through nested structs exactly like value keys, so the file is neither
// silently dropped nor rejected as unknown under strict mode.
type nestedAvatar struct {
	Avatar File   `form:"avatar"`
	Docs   []File `form:"docs"`
	Extra  *File  `form:"extra"`
}

type nestedUploadReq struct {
	Name string       `form:"name"`
	Meta nestedAvatar `form:"meta"`
}

type nestedUploadPtrReq struct {
	Name string        `form:"name"`
	Meta *nestedAvatar `form:"meta"`
}

func TestMarshalUnmarshal_RoundTrip_NestedFile(t *testing.T) {
	req := nestedUploadReq{
		Name: "n",
		Meta: nestedAvatar{
			Avatar: File{Content: []byte("a1"), Filename: "a.png"},
			Docs: []File{
				{Content: []byte("d1"), Filename: "d1.txt"},
				{Content: []byte("d2"), Filename: "d2.txt"},
			},
			Extra: &File{Content: []byte("e1"), Filename: "e.bin"},
		},
	}

	body, ct, err := Marshal(req)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got nestedUploadReq
	if err := Unmarshal(body, ct, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.Meta.Avatar.Filename != "a.png" || string(got.Meta.Avatar.Content) != "a1" {
		t.Errorf("Avatar = %+v, want a.png/a1", got.Meta.Avatar)
	}
	if len(got.Meta.Docs) != 2 || got.Meta.Docs[1].Filename != "d2.txt" {
		t.Errorf("Docs = %+v, want 2 files ending d2.txt", got.Meta.Docs)
	}
	if got.Meta.Extra == nil || string(got.Meta.Extra.Content) != "e1" {
		t.Errorf("Extra = %+v, want content e1", got.Meta.Extra)
	}

	// Strict mode must accept the encoder's own output.
	var strict nestedUploadReq
	if err := Unmarshal(body, ct, &strict, WithStrictUnmarshal(true)); err != nil {
		t.Fatalf("strict Unmarshal: %v", err)
	}
	if strict.Meta.Avatar.Filename != "a.png" {
		t.Errorf("strict Avatar = %+v, want a.png", strict.Meta.Avatar)
	}
}

func TestMarshalUnmarshal_RoundTrip_NestedFilePointer(t *testing.T) {
	req := nestedUploadPtrReq{
		Name: "n",
		Meta: &nestedAvatar{Avatar: File{Content: []byte("x"), Filename: "a.png"}},
	}

	body, ct, err := Marshal(req)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got nestedUploadPtrReq
	if err := Unmarshal(body, ct, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.Meta == nil || got.Meta.Avatar.Filename != "a.png" {
		t.Errorf("Meta = %+v, want non-nil with avatar a.png", got.Meta)
	}

	var strict nestedUploadPtrReq
	if err := Unmarshal(body, ct, &strict, WithStrictUnmarshal(true)); err != nil {
		t.Fatalf("strict Unmarshal: %v", err)
	}
	if strict.Meta == nil || string(strict.Meta.Avatar.Content) != "x" {
		t.Errorf("strict Meta = %+v, want non-nil avatar", strict.Meta)
	}
}

// A nested required field stays satisfied when the value arrives alongside a
// file part (the multipart provided-key set must include both value and file
// keys at their full nesting).
func TestFile_RequiredNestedProvidedWithPart(t *testing.T) {
	type req struct {
		ShipTo struct {
			City string `form:"city,required"`
		} `form:"ship_to"`
		Avatar File `form:"avatar"`
	}

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField("ship_to.city", "Lyon"); err != nil {
		t.Fatalf("write value: %v", err)
	}
	fw, err := mw.CreateFormFile("avatar", "a.txt")
	if err != nil {
		t.Fatalf("create file: %v", err)
	}
	if _, err := fw.Write([]byte("x")); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	var v req
	if err := Unmarshal(buf.Bytes(), mw.FormDataContentType(), &v); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if v.ShipTo.City != "Lyon" {
		t.Errorf("City = %q, want Lyon", v.ShipTo.City)
	}
	if v.Avatar.Filename != "a.txt" {
		t.Errorf("Avatar = %+v, want a.txt", v.Avatar)
	}
}

// Regressions (Issue 9): a File with an empty Filename (e.g. the zero value)
// produces a filename="" multipart part that the library itself refuses to
// decode. The encoder now skips such parts, so the body round-trips cleanly.
func TestFile_MarshalSkipsWithoutFilename(t *testing.T) {
	type s struct {
		Avatar File   `form:"avatar"`
		Docs   []File `form:"docs"`
	}
	v := s{
		Avatar: File{Content: []byte("keep"), Filename: "a.png"},
		Docs: []File{
			{Content: []byte("dropped")}, // no filename: skipped
			{Content: []byte("kept"), Filename: "b.txt"},
		},
	}
	body, ct, err := Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req.Header.Set("Content-Type", ct)
	if err := req.ParseMultipartForm(1 << 20); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := len(req.MultipartForm.File["avatar"]); got != 1 {
		t.Errorf("avatar parts = %d, want 1", got)
	}
	if got := len(req.MultipartForm.File["docs"]); got != 1 {
		t.Fatalf("docs parts = %d, want 1 (empty-filename part skipped)", got)
	}
	if req.MultipartForm.File["docs"][0].Filename != "b.txt" {
		t.Errorf("docs part = %q, want b.txt", req.MultipartForm.File["docs"][0].Filename)
	}
}

func TestFile_ZeroBodyDecodable(t *testing.T) {
	type s struct {
		Avatar File `form:"avatar"`
	}
	body, ct, err := Marshal(s{})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out s
	if err := Unmarshal(body, ct, &out); err != nil {
		t.Fatalf("zero-value File body must round-trip: %v", err)
	}
}

// Regression (Bug 5): File fields promoted by a pointer-embedded struct are
// written as flattened part names ("avatar") and must route back to the outer
// File field on decode, matching value-embed behavior.
type PtrFileMeta struct {
	Avatar File `form:"avatar"`
}

type ptrFileReq struct {
	*PtrFileMeta
	Name string `form:"name"`
}

func TestMarshalUnmarshal_RoundTrip_PointerEmbeddedFile(t *testing.T) {
	req := ptrFileReq{
		PtrFileMeta: &PtrFileMeta{Avatar: File{Content: []byte("ff"), Filename: "f.bin"}},
		Name:        "n",
	}
	body, ct, err := Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	mpReq := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	mpReq.Header.Set("Content-Type", ct)
	if err := mpReq.ParseMultipartForm(1 << 20); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, ok := mpReq.MultipartForm.File["avatar"]; !ok {
		t.Fatalf("expected flattened part name \"avatar\", got parts %v", mpReq.MultipartForm.File)
	}
	if _, ok := mpReq.MultipartForm.File["PtrFileMeta.avatar"]; ok {
		t.Errorf("unexpected dotted part name present")
	}

	var got ptrFileReq
	if err := Unmarshal(body, ct, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.PtrFileMeta == nil || got.Avatar.Filename != "f.bin" {
		t.Errorf("got = %+v, want non-nil embed with avatar f.bin", got)
	}
	if got.Name != "n" {
		t.Errorf("Name = %q, want n", got.Name)
	}

	// Strict mode must accept the encoder's own output.
	var strict ptrFileReq
	if err := Unmarshal(body, ct, &strict, WithStrictUnmarshal(true)); err != nil {
		t.Fatalf("strict unmarshal: %v", err)
	}
	if strict.Avatar.Filename != "f.bin" {
		t.Errorf("strict avatar = %+v, want f.bin", strict.Avatar)
	}
}

// Regression (round-2 gap 1): a File inside a slice-of-structs is emitted as
// "docs[0].bin" and must route back to that slice element on decode — it is
// neither dropped nor rejected as unknown under strict mode.
type sliceDoc struct {
	Name string `form:"name"`
	Bin  File   `form:"bin"`
}

type slicePack struct {
	Docs []sliceDoc `form:"docs"`
}

func TestMarshalUnmarshal_RoundTrip_FileInSliceOfStructs(t *testing.T) {
	in := slicePack{Docs: []sliceDoc{{
		Name: "a",
		Bin:  File{Content: []byte("x"), Filename: "a.bin"},
	}}}
	body, ct, err := Marshal(in)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got slicePack
	if err := Unmarshal(body, ct, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(got.Docs) != 1 || got.Docs[0].Name != "a" || string(got.Docs[0].Bin.Content) != "x" {
		t.Fatalf("file inside slice-of-structs lost: %+v", got.Docs)
	}

	var strict slicePack
	if err := Unmarshal(body, ct, &strict, WithStrictUnmarshal(true)); err != nil {
		t.Fatalf("strict Unmarshal: %v", err)
	}
	if len(strict.Docs) != 1 || string(strict.Docs[0].Bin.Content) != "x" {
		t.Fatalf("strict file inside slice-of-structs lost: %+v", strict.Docs)
	}
}

// Regression (round-2 gap 2): a File stored directly in a map value is emitted
// as "m[k]" and must route back to that map entry instead of leaving the map
// empty.
type mapFilePack struct {
	M map[string]File `form:"m"`
}

func TestMarshalUnmarshal_RoundTrip_FileInMap(t *testing.T) {
	in := mapFilePack{M: map[string]File{
		"k": {Content: []byte("x"), Filename: "k.bin"},
	}}
	body, ct, err := Marshal(in)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got mapFilePack
	if err := Unmarshal(body, ct, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	f, ok := got.M["k"]
	if !ok || string(f.Content) != "x" {
		t.Fatalf("file inside map lost: %+v", got.M)
	}

	var strict mapFilePack
	if err := Unmarshal(body, ct, &strict, WithStrictUnmarshal(true)); err != nil {
		t.Fatalf("strict Unmarshal: %v", err)
	}
	if s, ok := strict.M["k"]; !ok || string(s.Content) != "x" {
		t.Fatalf("strict file inside map lost: %+v", strict.M)
	}
}

// Regression (round-2 gap 2): a File promoted inside a map-of-struct value is
// emitted as "m[k].bin" and must route back to that nested field.
type mapStructPack struct {
	M map[string]sliceDoc `form:"m"`
}

func TestMarshalUnmarshal_RoundTrip_FileInMapStruct(t *testing.T) {
	in := mapStructPack{M: map[string]sliceDoc{
		"k": {Name: "n", Bin: File{Content: []byte("y"), Filename: "y.bin"}},
	}}
	body, ct, err := Marshal(in)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got mapStructPack
	if err := Unmarshal(body, ct, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	d, ok := got.M["k"]
	if !ok || d.Name != "n" || string(d.Bin.Content) != "y" {
		t.Fatalf("file inside map-of-struct lost: %+v", got.M)
	}

	var strict mapStructPack
	if err := Unmarshal(body, ct, &strict, WithStrictUnmarshal(true)); err != nil {
		t.Fatalf("strict Unmarshal: %v", err)
	}
	if s, ok := strict.M["k"]; !ok || string(s.Bin.Content) != "y" {
		t.Fatalf("strict file inside map-of-struct lost: %+v", strict.M)
	}
}

type uploadStruct struct {
	Title  string `form:"title"`
	Avatar File   `form:"avatar"`
	Docs   []File `form:"documents"`
}

func TestFile_MarshalMultipartFiles(t *testing.T) {
	enc := NewEncoder()
	in := uploadStruct{
		Title:  "hello",
		Avatar: File{Content: []byte("png-data"), ContentType: "image/png", Filename: "a.png"},
		Docs: []File{
			{Content: []byte("doc1"), ContentType: "text/plain", Filename: "d1.txt"},
			{Content: []byte("doc2"), ContentType: "text/plain", Filename: "d2.txt"},
		},
	}

	body, contentType, err := enc.MarshalMultipart(in)
	if err != nil {
		t.Fatalf("marshal multipart error: %v", err)
	}

	// Parse back via an http request.
	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	if err := req.ParseMultipartForm(1 << 20); err != nil {
		t.Fatalf("parse multipart: %v", err)
	}

	if req.FormValue("title") != "hello" {
		t.Errorf("title = %q", req.FormValue("title"))
	}

	var out uploadStruct
	dec := NewDecoder()
	if err := dec.UnmarshalMultipart(req, &out); err != nil {
		t.Fatalf("unmarshal multipart error: %v", err)
	}

	if out.Title != "hello" {
		t.Errorf("Title = %q", out.Title)
	}
	if string(out.Avatar.Content) != "png-data" {
		t.Errorf("Avatar.Content = %q", out.Avatar.Content)
	}
	if out.Avatar.Filename != "a.png" {
		t.Errorf("Avatar.Filename = %q", out.Avatar.Filename)
	}
	if len(out.Docs) != 2 {
		t.Fatalf("Docs len = %d", len(out.Docs))
	}
	if out.Docs[0].Filename != "d1.txt" || string(out.Docs[1].Content) != "doc2" {
		t.Errorf("Docs = %+v", out.Docs)
	}
}

func TestFile_FromRequestNoFiles(t *testing.T) {
	req := httptest.NewRequest("POST", "/", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := req.ParseForm(); err != nil {
		t.Fatalf("parse form: %v", err)
	}
	files, err := FilesFromRequest(req, "nonexistent")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if files != nil {
		t.Errorf("expected nil files, got %v", files)
	}
}

func TestFile_FromHeaderDetectContentType(t *testing.T) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("f", "x.txt")
	_, _ = fw.Write([]byte("hello file content"))
	_ = mw.Close()

	req := httptest.NewRequest("POST", "/", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if err := req.ParseMultipartForm(1 << 20); err != nil {
		t.Fatalf("parse: %v", err)
	}

	f, err := FileFromHeader(req.MultipartForm.File["f"][0])
	if err != nil {
		t.Fatalf("FileFromHeader: %v", err)
	}
	if string(f.Content) != "hello file content" {
		t.Errorf("content = %q", f.Content)
	}
	if f.ContentType == "" {
		t.Error("expected detected content type")
	}
}
