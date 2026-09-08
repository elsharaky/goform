package anyform

import (
	"bytes"
	"errors"
	"mime/multipart"
	"net/http/httptest"
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

// A value part applied to a File field means the client sent the part without
// a filename, so the error must say so — not the old cryptic notices.
func TestFileFieldValuePartClearError(t *testing.T) {
	type S struct {
		Avatar File `form:"avatar"`
	}

	body, ct := buildAliasValueBody(t, "avatar", "plain")
	var v S
	err := Unmarshal(body, ct, &v)
	if err == nil {
		t.Fatal("expected error")
	}
	var de *DecodingError
	if !errors.As(err, &de) {
		t.Fatalf("expected DecodingError, got %T: %v", err, err)
	}
	if de.FieldPath != "avatar" {
		t.Errorf("FieldPath = %q, want %q", de.FieldPath, "avatar")
	}
	if got := de.Err.Error(); got != "cannot decode value part into File field: multipart file parts must include a filename" {
		t.Fatalf("unexpected message: %q", got)
	}
}

func TestFileSliceFieldValuePartClearError(t *testing.T) {
	type S struct {
		Docs []File `form:"docs"`
	}

	body, ct := buildAliasValueBody(t, "docs", "plain")
	var v S
	err := Unmarshal(body, ct, &v)
	if err == nil {
		t.Fatal("expected error")
	}
	var de *DecodingError
	if !errors.As(err, &de) {
		t.Fatalf("expected DecodingError, got %T: %v", err, err)
	}
	if de.FieldPath != "docs" {
		t.Errorf("FieldPath = %q, want %q", de.FieldPath, "docs")
	}
	if got := de.Err.Error(); got != "cannot decode value part into []File field: multipart file parts must include a filename" {
		t.Fatalf("unexpected message: %q", got)
	}
}

func TestFilePtrFieldValuePartClearError(t *testing.T) {
	type S struct {
		Avatar *File `form:"avatar"`
	}

	body, ct := buildAliasValueBody(t, "avatar", "plain")
	var v S
	err := Unmarshal(body, ct, &v)
	if err == nil {
		t.Fatal("expected error")
	}
	var de *DecodingError
	if !errors.As(err, &de) {
		t.Fatalf("expected DecodingError, got %T: %v", err, err)
	}
	if de.FieldPath != "avatar" {
		t.Errorf("FieldPath = %q, want %q", de.FieldPath, "avatar")
	}
	if got := de.Err.Error(); got != "cannot decode value part into File field: multipart file parts must include a filename" {
		t.Fatalf("unexpected message: %q", got)
	}
}

// Bug #1: file fields must accept any tag key (form, json, xml, protobuf, Go name),
// matching how value fields behave.
func TestFileFieldAcceptsJSONTagKey(t *testing.T) {
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

func TestFileSliceAcceptsJSONTagKey(t *testing.T) {
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

func TestFilePtrAcceptsJSONTagKey(t *testing.T) {
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
func TestStrictUnmarshalRejectsUnknownFilePart(t *testing.T) {
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

func TestStrictUnmarshalAcceptsKnownFilePart(t *testing.T) {
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

func TestStrictUnmarshalAcceptsKnownFileAlias(t *testing.T) {
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
func TestStrictUnknownFilePartViaDecoder(t *testing.T) {
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

func TestUnmarshal_EmbeddedStructFileMultipart(t *testing.T) {
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

func TestUnmarshal_AmbiguousFilePartSiblings(t *testing.T) {
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

func TestUnmarshal_AmbiguousFilePartEmbedded(t *testing.T) {
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
func TestRequiredNestedProvidedWithFilePart(t *testing.T) {
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
func TestMarshal_SkipsFileWithoutFilename(t *testing.T) {
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

func TestMarshal_ZeroFileBodyDecodable(t *testing.T) {
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
