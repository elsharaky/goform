package anyform

import (
	"encoding"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
)

// keyToken is a parsed segment of a submitted form key.
type keyToken struct {
	kind string // "field", "index", "mapkey"
	name string
}

// parseKeyPath splits a form key into its path tokens.
// Examples:
//
//	"name"            -> [{field name}]
//	"address.city"    -> [{field address} {field city}]
//	"items[0].name"   -> [{field items} {index 0} {field name}]
//	"attr[key]"       -> [{field attr} {mapkey key}]
//	"matrix[0][1]"    -> [{field matrix} {index 0} {index 1}]
//
// Returns the base field name and the remaining tokens.
func parseKeyPath(key string) (base string, rest []keyToken) {
	var tokens []keyToken
	var name strings.Builder
	i := 0
	flush := func() {
		if name.Len() > 0 {
			tokens = append(tokens, keyToken{kind: "field", name: name.String()})
			name.Reset()
		}
	}

	for i < len(key) {
		c := key[i]
		switch c {
		case '.':
			flush()
			i++
		case '[':
			flush()
			end := strings.IndexByte(key[i:], ']')
			if end < 0 {
				name.WriteString(key[i:])
				flush()
				i = len(key)
				continue
			}
			inner := key[i+1 : i+end]
			if n, err := strconv.Atoi(inner); err == nil {
				tokens = append(tokens, keyToken{kind: "index", name: strconv.Itoa(n)})
			} else {
				tokens = append(tokens, keyToken{kind: "mapkey", name: inner})
			}
			i = i + end + 1
		default:
			name.WriteByte(c)
			i++
		}
	}
	flush()

	if len(tokens) == 0 {
		return "", nil
	}
	return tokens[0].name, tokens[1:]
}

// Decoder unmarshals url.Values or multipart form data into Go structs.
// It is safe for concurrent use after construction.
type Decoder struct {
	cfg      *config
	resolver *tagResolver
}

// NewDecoder creates a Decoder with the given options.
func NewDecoder(opts ...Option) *Decoder {
	cfg := defaultConfig()
	for _, opt := range opts {
		if opt != nil {
			opt(cfg)
		}
	}
	return &Decoder{
		cfg:      cfg,
		resolver: newTagResolver(cfg.tagPriority...),
	}
}

// Unmarshal converts url.Values into the struct pointed to by v.
// v must be a non-nil pointer to a struct.
func (d *Decoder) Unmarshal(values url.Values, v any) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return &DecodingError{Err: errors.New("expected non-nil pointer to struct")}
	}
	elem := rv.Elem()
	if elem.Kind() != reflect.Struct {
		return &DecodingError{Err: ErrNotStruct}
	}
	if err := d.unmarshalValues(values, elem, 0); err != nil {
		return err
	}
	return d.applyDefaultsAndRequired(elem, d.providedFields(elem, formKeys(values), 0), nil, 0)
}

// UnmarshalMultipart parses an http.Request's multipart form into the struct
// pointed to by v. File fields of type File/[]File are populated from the
// multipart file parts. The request must have been parsed with
// ParseMultipartForm beforehand.
func (d *Decoder) UnmarshalMultipart(r *http.Request, v any) error {
	return d.UnmarshalMultipartForm(r.MultipartForm, v)
}

// UnmarshalMultipartForm parses a multipart.Form into the struct pointed to by v,
// populating both scalar and File fields.
func (d *Decoder) UnmarshalMultipartForm(mf *multipart.Form, v any) error {
	if mf == nil {
		return &DecodingError{Err: errors.New("anyform: nil multipart form")}
	}

	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return &DecodingError{Err: errors.New("expected non-nil pointer to struct")}
	}
	elem := rv.Elem()
	if elem.Kind() != reflect.Struct {
		return &DecodingError{Err: ErrNotStruct}
	}

	values := url.Values(mf.Value)
	if err := d.unmarshalValues(values, elem, 0); err != nil {
		return err
	}
	if err := d.unmarshalFiles(mf, elem); err != nil {
		return err
	}
	keys := formKeys(values)
	for k := range mf.File {
		keys = append(keys, k)
	}
	return d.applyDefaultsAndRequired(elem, d.providedFields(elem, keys, 0), nil, 0)
}

// unmarshalValues iterates over the submitted keys and assigns values,
// building a fresh field index for the struct level.
func (d *Decoder) unmarshalValues(values url.Values, dst reflect.Value, depth int) error {
	if depth > d.cfg.maxDepth {
		return &DecodingError{Err: ErrMaxDepthExceeded}
	}

	index := d.resolver.buildUnmarshalIndex(dst.Type())

	for key, vals := range values {
		base, rest := parseKeyPath(key)
		if index.ambiguous[base] {
			return &DecodingError{Key: key,
				Err: fmt.Errorf("ambiguous field %q matches more than one field", base)}
		}
		field, ok := index.fields[base]
		if !ok {
			if d.cfg.strict {
				return &DecodingError{Key: key, Err: fmt.Errorf("unknown field %q", base)}
			}
			continue
		}

		fieldVal := fieldByIndexAlloc(dst, field.Index)
		if !fieldVal.CanSet() {
			continue
		}

		if err := d.decodePath(fieldVal, rest, vals, depth+1, base); err != nil {
			return err
		}
	}

	return nil
}

// fieldByIndexAlloc resolves a struct field by index path, allocating nil
// embedded pointers along the way so promoted fields of pointer-embedded
// structs can be written (mirroring encoding/json). If a nil embedded pointer
// cannot be allocated (unexported field), it is returned as-is.
func fieldByIndexAlloc(v reflect.Value, index []int) reflect.Value {
	for _, i := range index {
		for v.Kind() == reflect.Pointer {
			if v.IsNil() {
				if !v.CanSet() {
					return v
				}
				v.Set(reflect.New(v.Type().Elem()))
			}
			v = v.Elem()
		}
		v = v.Field(i)
	}
	return v
}

// fieldByIndexRO resolves a struct field by index path without writing; it
// reports false when a nil embedded pointer blocks traversal.
func fieldByIndexRO(v reflect.Value, index []int) (reflect.Value, bool) {
	for _, i := range index {
		for v.Kind() == reflect.Pointer {
			if v.IsNil() {
				return reflect.Value{}, false
			}
			v = v.Elem()
		}
		v = v.Field(i)
	}
	return v, true
}

// decodePath walks the parsed key tokens and assigns the leaf value(s).
// path is the accumulated form key (e.g. "address.city" or "items[3]") used
// to give decode errors a meaningful FieldPath.
func (d *Decoder) decodePath(field reflect.Value, rest []keyToken, vals []string, depth int, path string) error {
	if depth > d.cfg.maxDepth {
		return &DecodingError{Err: ErrMaxDepthExceeded}
	}

	// Dereference pointers as we descend.
	if field.Kind() == reflect.Pointer {
		// A value part routed to a *File / *[]File field is an untouched
		// browser file input (no filename). Skip it BEFORE allocating the
		// pointed-to value: unlike ordinary pointer autovivification there is
		// nothing to assign — file fields are only populated from real file
		// parts — so leave the pointer nil instead of a zero File.
		if field.Type().Elem() == reflect.TypeOf(File{}) ||
			field.Type().Elem() == reflect.TypeOf([]File{}) {
			return nil
		}
		if field.IsNil() {
			field.Set(reflect.New(field.Type().Elem()))
		}
		field = field.Elem()
	}

	if len(rest) == 0 {
		// Leaf: assign value(s) to this field. Keep the error type consistent
		// with every other decode path — a scalar parse failure (int overflow,
		// bad bool, ...) must still satisfy errors.As(..., &DecodingError{}).
		if err := d.assignLeaf(field, vals); err != nil {
			var de *DecodingError
			if errors.As(err, &de) {
				return de
			}
			return &DecodingError{FieldPath: path, Err: err}
		}
		return nil
	}

	head := rest[0]
	tail := rest[1:]

	switch head.kind {
	case "field":
		// Descend into a named sub-field.
		switch field.Kind() {
		case reflect.Struct:
			if field.Type() == reflect.TypeOf(File{}) {
				return &DecodingError{Err: errors.New("cannot descend into File; use UnmarshalMultipartForm")}
			}
			childIndex := d.resolver.buildUnmarshalIndex(field.Type())
			if childIndex.ambiguous[head.name] {
				return &DecodingError{FieldPath: head.name,
					Err: fmt.Errorf("ambiguous field %q matches more than one field", head.name)}
			}
			child, ok := childIndex.fields[head.name]
			if !ok {
				if d.cfg.strict {
					return &DecodingError{FieldPath: head.name, Err: errors.New("unknown field")}
				}
				return nil
			}
			childVal := fieldByIndexAlloc(field, child.Index)
			return d.decodePath(childVal, tail, vals, depth+1, path+"."+head.name)
		case reflect.Slice, reflect.Array:
			// A dotted key on a container is never produced by any encoder,
			// which addresses elements only as "slice[i]". Previously this
			// re-dispatched to the leaf and silently APPENDED the submitted
			// value to the slice while discarding the segment name — so a key
			// like "lines.evil" wrote attacker data into the lines field and
			// slipped past strict mode. Treat it as a structural mismatch like
			// any other: ignored in non-strict mode, rejected in strict mode.
			if d.cfg.strict {
				return &DecodingError{FieldPath: head.name,
					Err: errors.New("cannot descend into slice/array without an index")}
			}
			return nil
		case reflect.Map:
			// "map.field" (unbracketed) is likewise never produced by the
			// encoder, which uses "map[key]" exclusively.
			if d.cfg.strict {
				return &DecodingError{FieldPath: head.name,
					Err: errors.New("cannot descend into map without bracket notation")}
			}
			return nil
		default:
			if d.cfg.strict {
				return &DecodingError{FieldPath: head.name, Err: errors.New("cannot descend into scalar")}
			}
			return nil
		}

	case "index":
		// A "[123]" token on a Map is a string map key, not an index: the
		// encoder renders numeric-keyed maps as "m[123]"/"m[-1]" (parseKeyPath
		// classifies every numeric bracket as an index). Routing it through the
		// map-key path makes numeric-keyed maps round-trip instead of being
		// silently dropped.
		if field.Kind() == reflect.Map {
			return d.consumeMapKey(field, head.name, tail, vals, depth, path)
		}

		if field.Kind() != reflect.Slice && field.Kind() != reflect.Array {
			if d.cfg.strict {
				return &DecodingError{FieldPath: head.name, Err: errors.New("index on non-indexable field")}
			}
			return nil
		}
		idx, err := strconv.Atoi(head.name)
		if err != nil {
			return &DecodingError{FieldPath: head.name, Err: fmt.Errorf("invalid index %q", head.name)}
		}
		if idx < 0 {
			return &DecodingError{FieldPath: head.name, Err: fmt.Errorf("negative index %d", idx)}
		}

		if field.Kind() == reflect.Array {
			// Fixed-size arrays cannot grow: reject out-of-range indices
			// instead of attempting to append (which would panic).
			if idx >= field.Len() {
				return &DecodingError{FieldPath: head.name,
					Err: fmt.Errorf("index %d out of range for array of length %d", idx, field.Len())}
			}
		} else {
			// Slice: cap how far a client-supplied index can grow a slice.
			// Without a bound, a tiny body like "items[5000000]=x" forces a
			// huge allocation (memory-exhaustion DoS) that bypasses
			// WithMaxBodySize.
			if d.cfg.maxSliceIndex > 0 && idx >= d.cfg.maxSliceIndex {
				return &DecodingError{FieldPath: head.name,
					Err: fmt.Errorf("index %d exceeds maximum slice index %d", idx, d.cfg.maxSliceIndex)}
			}
			for field.Len() <= idx {
				field.Set(reflect.Append(field, reflect.New(field.Type().Elem()).Elem()))
			}
		}
		elem := field.Index(idx)
		return d.decodePath(elem, tail, vals, depth+1, path+"["+head.name+"]")

	case "mapkey":
		if field.Kind() != reflect.Map {
			if d.cfg.strict {
				return &DecodingError{FieldPath: head.name, Err: errors.New("mapkey on non-map field")}
			}
			return nil
		}
		return d.consumeMapKey(field, head.name, tail, vals, depth, path)

	default:
		return &DecodingError{FieldPath: head.name, Err: errors.New("unknown key token")}
	}
}

// consumeMapKey resolves one bracket token against a Map field: it parses the
// key, then either assigns a leaf value directly to the mapped element or
// descends into the element so distinct keys targeting the same entry merge.
// It is shared by "[key]" tokens and numeric "[i]" tokens on a map (the latter
// being string keys of numerically-keyed maps).
func (d *Decoder) consumeMapKey(field reflect.Value, rawKey string, tail []keyToken, vals []string, depth int, path string) error {
	if field.IsNil() {
		field.Set(reflect.MakeMap(field.Type()))
	}
	mapKey := reflect.New(field.Type().Key()).Elem()
	if err := d.assignScalarTo(mapKey, rawKey); err != nil {
		return &DecodingError{FieldPath: rawKey, Err: err}
	}

	if len(tail) == 0 {
		// Leaf map value. A scalar targeting a struct element (or anything
		// else that cannot consume a leaf) surfaces as an error via
		// assignLeaf instead of silently inserting a zero value.
		val := reflect.New(field.Type().Elem()).Elem()
		if err := d.assignLeaf(val, vals); err != nil {
			return &DecodingError{FieldPath: rawKey, Err: err}
		}
		field.SetMapIndex(mapKey, val)
		return nil
	}

	// Map value is complex (struct/slice); descending requires a settable
	// value. Start from an existing entry so distinct keys targeting the
	// same element ("m[k].name", "m[k].age", or a file part "m[k].bin")
	// merge instead of overwriting one another, then write the result back.
	val := reflect.New(field.Type().Elem()).Elem()
	if existing := field.MapIndex(mapKey); existing.IsValid() {
		val.Set(existing)
	}
	if err := d.decodePath(val, tail, vals, depth+1, path); err != nil {
		return &DecodingError{FieldPath: rawKey, Err: err}
	}
	field.SetMapIndex(mapKey, val)
	return nil
}

// assignLeaf assigns raw string values to a leaf field of any supported kind.
func (d *Decoder) assignLeaf(field reflect.Value, vals []string) error {
	if len(vals) == 0 {
		return nil
	}
	if field.Kind() == reflect.Pointer {
		if field.IsNil() {
			field.Set(reflect.New(field.Type().Elem()))
		}
		field = field.Elem()
	}

	// Registered converters take priority over kind-based handling, matching
	// the encoder. This matters for net.IP (slice kind) and url.URL (struct
	// kind), which otherwise fall into the slice/struct branches below.
	if conv, ok := d.cfg.converters[field.Type()]; ok {
		return conv.Unmarshal(vals[0], field)
	}

	switch field.Kind() {
	case reflect.Struct:
		// A value part named like a File field is the signature of an
		// untouched browser file input, whose part carries an empty filename
		// and is routed by the multipart parser to the value store. Failing
		// the whole decode for an optional file box that the user left empty
		// would break an ordinary form. The encoder already skips File fields
		// without a filename, so parity here is to ignore the stray value part.
		if field.Type() == reflect.TypeOf(File{}) {
			return nil
		}
		if field.Type() == reflect.TypeOf(time.Time{}) {
			tv := timeConverter{layout: d.cfg.timeLayout}
			if err := tv.Unmarshal(vals[0], field); err != nil {
				return &DecodingError{FieldPath: "", Err: err}
			}
			return nil
		}
		// Symmetric with the encoder: a struct leaf that implements
		// encoding.TextUnmarshaler (and whose encoder text-marshals it) is
		// decoded through it.
		if d.cfg.textAware && field.CanAddr() {
			if tu, ok := field.Addr().Interface().(encoding.TextUnmarshaler); ok {
				return tu.UnmarshalText([]byte(vals[0]))
			}
		}
		// A lone scalar against a plain struct field is a client error, not
		// silent data loss: previously the value was discarded and the field
		// stayed zero, masking mistakes like "m[k]=v" on a map of structs or a
		// text-marshaled value whose target lacks a TextUnmarshaler.
		return &DecodingError{FieldPath: "", Err: errors.New("cannot assign scalar value to struct field without nested keys")}
	case reflect.Slice, reflect.Array:
		// A value part applied to a []File field is the multi-file analogue of
		// the untouched file input above; ignore it rather than fail the
		// decode, mirroring the encoder's skip of empty-filename files.
		if field.Type() == reflect.TypeOf([]File{}) {
			return nil
		}
		// A raw []byte is a scalar blob, not a slice of numbers: accept the
		// conventional single-value form ("data=<bytes>") that the encoder
		// now emits. Explicit "[i]" keys keep working through decodePath's
		// index branch, which reaches assignScalarTo per element.
		if field.Type() == reflect.TypeOf([]byte{}) {
			if len(vals) > 1 {
				return errors.New("[]byte expects a single value")
			}
			if len(vals) == 1 {
				field.SetBytes([]byte(vals[0]))
			}
			return nil
		}
		// Repeated key: append each value as new element.
		if field.Kind() == reflect.Slice {
			for _, v := range vals {
				elem := reflect.New(field.Type().Elem()).Elem()
				if err := d.assignScalarTo(elem, v); err != nil {
					return &DecodingError{FieldPath: "", Err: err}
				}
				field.Set(reflect.Append(field, elem))
			}
			return nil
		}
		// Array: set by position.
		for i, v := range vals {
			if i >= field.Len() {
				break
			}
			if err := d.assignScalarTo(field.Index(i), v); err != nil {
				return &DecodingError{FieldPath: "", Err: err}
			}
		}
		return nil
	case reflect.Map:
		// A map reached directly without key -> unsupported.
		return &DecodingError{FieldPath: "", Err: errors.New("map requires bracket notation")}
	default:
		return d.assignScalarTo(field, vals[0])
	}
}

// assignScalarTo assigns a single string to a target field, honoring custom
// converters and TextUnmarshaler. Converters take priority so slice elements,
// map keys, and defaults follow the same precedence as full-leaf assignment
// (assignLeaf) and the encoder — a registered converter always wins.
func (d *Decoder) assignScalarTo(field reflect.Value, value string) error {
	if conv, ok := d.cfg.converters[field.Type()]; ok {
		return conv.Unmarshal(value, field)
	}
	if field.CanAddr() {
		if tu, ok := field.Addr().Interface().(encoding.TextUnmarshaler); ok && d.cfg.textAware {
			return tu.UnmarshalText([]byte(value))
		}
	}
	return parseScalar(value, field)
}

// readFile reads a single multipart file header into a File, honoring the
// configured per-file size limit. The size is checked against the part's
// declared size before its content is read into memory.
func (d *Decoder) readFile(fh *multipart.FileHeader, name string) (File, error) {
	if d.cfg.maxFileSize > 0 && fh.Size > d.cfg.maxFileSize {
		return File{}, &DecodingError{FieldPath: name, Err: ErrFileTooLarge}
	}
	f, err := FileFromHeader(fh)
	if err != nil {
		return File{}, &DecodingError{FieldPath: name, Err: err}
	}
	// Safety net: a manually constructed FileHeader may carry Size == 0 even
	// though its read content exceeds the limit.
	if d.cfg.maxFileSize > 0 && int64(len(f.Content)) > d.cfg.maxFileSize {
		return File{}, &DecodingError{FieldPath: name, Err: ErrFileTooLarge}
	}
	return f, nil
}

// unmarshalFiles populates File, []File, and *File fields from multipart file
// parts. Part names are routed through the destination struct exactly like
// value keys: a submitted part "meta.avatar" descends from the Meta field to
// its inner Avatar field, mirroring the dotted keys the encoder emits for
// nested structs (so nested File fields round-trip instead of being dropped).
// Each level resolves the part's base token through the flattened per-type
// index, which handles embedded promoted fields and every tag alias the same
// way unmarshalValues does.
func (d *Decoder) unmarshalFiles(mf *multipart.Form, dst reflect.Value) error {
	if len(mf.File) == 0 {
		return nil
	}

	consumed := make(map[string]bool, len(mf.File))
	// Deterministic order: a part name is unique, but two different names can
	// both resolve to the same field (e.g. "doc" and "Doc"); sorting keeps the
	// winning value stable across runs.
	parts := make([]string, 0, len(mf.File))
	for part := range mf.File {
		parts = append(parts, part)
	}
	sort.Strings(parts)

	for _, part := range parts {
		if consumed[part] {
			continue
		}
		base, rest := parseKeyPath(part)
		used, err := d.consumeFilePart(dst, base, rest, mf.File[part], part, 0)
		if err != nil {
			return err
		}
		if used {
			consumed[part] = true
		}
	}

	// Under strict mode, any multipart file part that did not map to a File
	// field is an error instead of being silently dropped.
	if d.cfg.strict {
		for part := range mf.File {
			if !consumed[part] {
				return &DecodingError{Key: part, Err: fmt.Errorf("unknown field %q", part)}
			}
		}
	}

	return nil
}

// consumeFilePart routes a single submitted file part into the File field it
// addresses. It returns whether the part was consumed (matched a File field).
// base is the first key token to resolve within dst; rest holds any remaining
// dotted/indexed tokens. Each level resolves names through the flattened
// per-type index, so a part "meta.avatar" resolves meta at the root and avatar
// inside the Meta struct. Indexed and mapped tokens are followed exactly like
// value keys, so file parts inside slices-of-structs ("docs[0].bin") and maps
// ("m[k]") round-trip instead of being dropped. Ambiguous bases — two sibling
// File fields sharing a tag, or an embedded promoted File plus an outer File
// sharing a tag — are rejected so the part is not consumed by every matching
// field.
func (d *Decoder) consumeFilePart(dst reflect.Value, base string, rest []keyToken, headers []*multipart.FileHeader, part string, depth int) (bool, error) {
	tokens := make([]keyToken, 0, len(rest)+1)
	tokens = append(tokens, keyToken{kind: "field", name: base})
	tokens = append(tokens, rest...)
	return d.consumeFilePartTokens(dst, tokens, headers, part, depth)
}

// consumeFilePartTokens walks one file part through the destination value,
// consuming index and mapkey tokens the same way decodePath does for value
// keys (growing slices, allocating map entries). The final token must resolve
// to a File, []File, or *File; anything else leaves the part unconsumed —
// dropped by default, rejected as an unknown field in strict mode.
func (d *Decoder) consumeFilePartTokens(cur reflect.Value, tokens []keyToken, headers []*multipart.FileHeader, part string, depth int) (bool, error) {
	if depth > d.cfg.maxDepth {
		return false, &DecodingError{Key: part, Err: ErrMaxDepthExceeded}
	}

	// Pointer-to-struct values are dereferenced (allocating when nil) so a
	// part can route through a *NestedMeta exactly like a value key would.
	// Pointer-to-File stays a leaf target.
	for cur.Kind() == reflect.Pointer && cur.Type().Elem() != reflect.TypeOf(File{}) {
		if cur.IsNil() {
			if !cur.CanSet() {
				return false, nil
			}
			cur.Set(reflect.New(cur.Type().Elem()))
		}
		cur = cur.Elem()
	}

	head := tokens[0]
	tail := tokens[1:]

	switch head.kind {
	case "field":
		if cur.Kind() != reflect.Struct || cur.Type() == reflect.TypeOf(File{}) {
			return false, nil
		}
		index := d.resolver.buildUnmarshalIndex(cur.Type())
		if index.ambiguous[head.name] {
			return false, &DecodingError{Key: part,
				Err: fmt.Errorf("ambiguous field %q matches more than one field", head.name)}
		}
		sf, ok := index.fields[head.name]
		if !ok {
			return false, nil
		}
		fieldVal := fieldByIndexAlloc(cur, sf.Index)
		if !fieldVal.CanSet() {
			return false, nil
		}
		if len(tail) == 0 {
			// Leaf: the part's final token names a File, []File, or *File field.
			return d.consumeFileLeaf(fieldVal, headers, part)
		}
		return d.consumeFilePartTokens(fieldVal, tail, headers, part, depth+1)

	case "index":
		if cur.Kind() != reflect.Slice && cur.Kind() != reflect.Array {
			return false, nil
		}
		idx, err := strconv.Atoi(head.name)
		if err != nil {
			return false, &DecodingError{Key: part, Err: fmt.Errorf("invalid index %q", head.name)}
		}
		if idx < 0 {
			return false, &DecodingError{Key: part, Err: fmt.Errorf("negative index %d", idx)}
		}
		if cur.Kind() == reflect.Array {
			// Fixed-size arrays cannot grow: an out-of-range index means the
			// part does not address a stored element.
			if idx >= cur.Len() {
				return false, nil
			}
		} else {
			if d.cfg.maxSliceIndex > 0 && idx >= d.cfg.maxSliceIndex {
				return false, nil
			}
			for cur.Len() <= idx {
				cur.Set(reflect.Append(cur, reflect.New(cur.Type().Elem()).Elem()))
			}
		}
		elem := cur.Index(idx)
		if len(tail) == 0 {
			return d.consumeFileLeaf(elem, headers, part)
		}
		return d.consumeFilePartTokens(elem, tail, headers, part, depth+1)

	case "mapkey":
		if cur.Kind() != reflect.Map {
			return false, nil
		}
		if cur.IsNil() {
			if !cur.CanSet() {
				return false, nil
			}
			cur.Set(reflect.MakeMap(cur.Type()))
		}
		mapKey := reflect.New(cur.Type().Key()).Elem()
		if err := d.assignScalarTo(mapKey, head.name); err != nil {
			return false, &DecodingError{Key: part, Err: err}
		}
		if len(tail) == 0 {
			// Leaf map value is a File, []File, or *File.
			val := reflect.New(cur.Type().Elem()).Elem()
			used, err := d.consumeFileLeaf(val, headers, part)
			if err != nil || !used {
				return false, err
			}
			cur.SetMapIndex(mapKey, val)
			return true, nil
		}
		// The map value is a struct; decode into a fresh settable value so the
		// nested leaf can be written, then hand the populated value to the map.
		// Start from an existing entry (written by a value part such as
		// "m[k].name") so the two passes merge rather than replace each other.
		val := reflect.New(cur.Type().Elem()).Elem()
		if existing := cur.MapIndex(mapKey); existing.IsValid() {
			val.Set(existing)
		}
		used, err := d.consumeFilePartTokens(val, tail, headers, part, depth+1)
		if err != nil || !used {
			return false, err
		}
		cur.SetMapIndex(mapKey, val)
		return true, nil

	default:
		return false, nil
	}
}

// consumeFileLeaf writes the file part's headers into an already-resolved
// File, []File, or *File field.
func (d *Decoder) consumeFileLeaf(fieldVal reflect.Value, headers []*multipart.FileHeader, part string) (bool, error) {
	switch fieldVal.Type() {
	case reflect.TypeOf(File{}):
		f, err := d.readFile(headers[0], part)
		if err != nil {
			return false, err
		}
		fieldVal.Set(reflect.ValueOf(f))
		return true, nil
	case reflect.TypeOf([]File{}):
		out := make([]File, 0, len(headers))
		for _, fh := range headers {
			f, err := d.readFile(fh, part)
			if err != nil {
				return false, err
			}
			out = append(out, f)
		}
		fieldVal.Set(reflect.ValueOf(out))
		return true, nil
	default:
		if fieldVal.Kind() == reflect.Pointer && fieldVal.Type().Elem() == reflect.TypeOf(File{}) {
			f, err := d.readFile(headers[0], part)
			if err != nil {
				return false, err
			}
			if fieldVal.IsNil() {
				fieldVal.Set(reflect.New(reflect.TypeOf(File{})))
			}
			fieldVal.Elem().Set(reflect.ValueOf(f))
			return true, nil
		}
		return false, nil
	}
}

// formKeys returns the submitted form keys as a slice (order irrelevant; the
// set drives provided-field tracking).
func formKeys(values url.Values) []string {
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	return keys
}

// providedFields returns the set of canonical field paths — joined
// reflect.StructField index chains — that the submitted keys decodes into.
// It mirrors decodePath's routing so default/required logic shares one notion
// of "provided" with the decoder itself: nested "ship_to.city" keys, promoted
// embedded fields, indexed slice keys, and alternate tag names (json:"reg"
// addressing a form:"region" field) all mark the destination field provided.
// Keys the decoder rejects (ambiguous, unknown, out of range, too deep) mark
// nothing, matching the fact that they decode to nothing.
func (d *Decoder) providedFields(dst reflect.Value, keys []string, depth int) map[string]bool {
	provided := make(map[string]bool, len(keys))
	for _, key := range keys {
		base, rest := parseKeyPath(key)
		if base == "" {
			continue
		}
		tokens := make([]keyToken, 0, len(rest)+1)
		tokens = append(tokens, keyToken{kind: "field", name: base})
		tokens = append(tokens, rest...)
		d.markProvidedPath(dst, tokens, nil, provided, depth)
	}
	return provided
}

// markProvidedPath routes a single parsed key through the destination struct,
// recording the canonical index path of every field it resolves to. Each
// level uses the same flattened per-type index as the value decoder.
func (d *Decoder) markProvidedPath(cur reflect.Value, tokens []keyToken, indexPath []int, provided map[string]bool, depth int) {
	if depth > d.cfg.maxDepth || len(tokens) == 0 {
		return
	}

	if cur.Kind() == reflect.Pointer {
		if cur.IsNil() {
			return
		}
		cur = cur.Elem()
	}

	head := tokens[0]
	tail := tokens[1:]

	switch head.kind {
	case "field":
		if cur.Kind() != reflect.Struct || cur.Type() == reflect.TypeOf(File{}) {
			return
		}
		index := d.resolver.buildUnmarshalIndex(cur.Type())
		if index.ambiguous[head.name] {
			return
		}
		sf, ok := index.fields[head.name]
		if !ok {
			return
		}
		fieldIndex := appendIndex(indexPath, sf.Index...)
		provided[joinIndex(fieldIndex)] = true
		if field, ok := fieldByIndexRO(cur, sf.Index); ok {
			d.markProvidedPath(field, tail, fieldIndex, provided, depth+1)
		}
	case "index":
		if cur.Kind() != reflect.Slice && cur.Kind() != reflect.Array {
			return
		}
		n, err := strconv.Atoi(head.name)
		if err != nil || n < 0 {
			return
		}
		if n >= cur.Len() {
			return
		}
		d.markProvidedPath(cur.Index(n), tail, indexPath, provided, depth+1)
	default:
		return
	}
}

// appendIndex clones an index path with extra elements appended. Slices are
// kept immutable so shared ancestors are never corrupted.
func appendIndex(path []int, extra ...int) []int {
	out := make([]int, 0, len(path)+len(extra))
	out = append(out, path...)
	out = append(out, extra...)
	return out
}

// joinIndex renders an index path as a compact, unambiguous string key.
func joinIndex(path []int) string {
	if len(path) == 0 {
		return ""
	}
	var b strings.Builder
	for i, n := range path {
		if i > 0 {
			b.WriteByte('/')
		}
		b.WriteString(strconv.Itoa(n))
	}
	return b.String()
}

// applyDefaultsAndRequired walks the destination struct after unmarshalling,
// setting default values for fields not provided and enforcing required fields.
// A field counts as provided when any submitted key resolved to it (see
// providedFields) — nested keys and alternate tag names included — so a field
// delivered as "ship_to.city" is never reported missing, and its default is
// never written over a provided value.
func (d *Decoder) applyDefaultsAndRequired(dst reflect.Value, provided map[string]bool, indexPath []int, depth int) error {
	if dst.Kind() == reflect.Pointer {
		if dst.IsNil() {
			return nil
		}
		dst = dst.Elem()
	}
	if dst.Kind() != reflect.Struct || depth > d.cfg.maxDepth {
		return nil
	}

	rt := dst.Type()
	for i := range rt.NumField() {
		sf := rt.Field(i)
		fieldVal := dst.Field(i)
		fieldIndex := appendIndex(indexPath, sf.Index...)

		if sf.Anonymous && sf.Type.Kind() == reflect.Struct {
			if sf.Type == reflect.TypeOf(File{}) {
				continue
			}
			if err := d.applyDefaultsAndRequired(fieldVal, provided, fieldIndex, depth+1); err != nil {
				return err
			}
			continue
		}

		// Anonymous pointer-to-struct embeds flatten like value embeds; a nil
		// embedded pointer carries no fields to walk. Only exported pointer
		// embeds are walked here, matching the unmarshal index and both
		// encoders (unexported value embeds flatten on every path).
		if sf.Anonymous && sf.IsExported() && sf.Type.Kind() == reflect.Pointer &&
			sf.Type.Elem().Kind() == reflect.Struct &&
			sf.Type.Elem() != reflect.TypeOf(File{}) {
			if fieldVal.IsNil() {
				continue
			}
			if err := d.applyDefaultsAndRequired(fieldVal, provided, fieldIndex, depth+1); err != nil {
				return err
			}
			continue
		}

		if !sf.IsExported() {
			continue
		}

		name, skip := d.resolver.marshalFieldName(sf)
		if skip {
			continue
		}

		opts := d.resolver.marshalFieldOptions(sf)

		// Recurse into nested structs so inner defaults/required apply too.
		switch {
		case fieldVal.Kind() == reflect.Pointer &&
			fieldVal.Type().Elem().Kind() == reflect.Struct &&
			fieldVal.Type().Elem() != reflect.TypeOf(File{}) &&
			!fieldVal.IsNil():
			if err := d.applyDefaultsAndRequired(fieldVal, provided, fieldIndex, depth+1); err != nil {
				return err
			}
		case fieldVal.Kind() == reflect.Struct &&
			fieldVal.Type() != reflect.TypeOf(File{}) &&
			fieldVal.Type() != reflect.TypeOf(time.Time{}):
			if err := d.applyDefaultsAndRequired(fieldVal, provided, fieldIndex, depth+1); err != nil {
				return err
			}
		}

		if provided[joinIndex(fieldIndex)] {
			continue
		}

		if opts.Required {
			return &DecodingError{Key: name, Err: ErrMissingRequired}
		}
		if opts.HasDefault && isDefaultable(fieldVal.Type()) {
			if err := d.assignScalarTo(fieldVal, opts.Default); err != nil {
				return &DecodingError{Key: name, Err: err}
			}
		}
	}

	return nil
}

// isDefaultable reports whether a type can receive a default value from a
// string. Pointers, slices, maps, and File types are intentionally excluded.
func isDefaultable(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.String, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128:
		return true
	default:
		return false
	}
}
