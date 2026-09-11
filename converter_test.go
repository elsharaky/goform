package goform

import (
	"net"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
)

type Currency int

const (
	USD Currency = iota
	EUR
)

type currencyConverter struct{}

func (currencyConverter) Marshal(v reflect.Value) (string, error) {
	switch v.Int() {
	case 0:
		return "USD", nil
	case 1:
		return "EUR", nil
	}
	return "UNKNOWN", nil
}

func (currencyConverter) Unmarshal(s string, f reflect.Value) error {
	switch strings.ToUpper(s) {
	case "USD":
		f.SetInt(0)
	case "EUR":
		f.SetInt(1)
	}
	return nil
}

type convertStruct struct {
	Price  Currency `form:"price"`
	Amount Currency `form:"amount"`
}

func TestConverter_CustomConverter(t *testing.T) {
	enc := NewEncoder(WithCustomConverter(reflect.TypeOf(Currency(0)), currencyConverter{}))
	in := convertStruct{Price: USD, Amount: EUR}
	vals, err := enc.Marshal(in)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	if vals.Get("price") != "USD" || vals.Get("amount") != "EUR" {
		t.Errorf("vals = %v", vals)
	}

	dec := NewDecoder(WithCustomConverter(reflect.TypeOf(Currency(0)), currencyConverter{}))
	var out convertStruct
	if err := dec.Unmarshal(url.Values{"price": {"EUR"}, "amount": {"USD"}}, &out); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if out.Price != EUR || out.Amount != USD {
		t.Errorf("out = %+v", out)
	}
}

// bothType both implements encoding.TextMarshaler/TextUnmarshaler and has a
// registered converter. A registered converter must win at every container
// depth (leaf, slice element, map value/key) on both encode and decode —
// previously slice elements routed through assignScalarTo, which checked
// TextUnmarshaler first and flipped the precedence with container depth.
type bothType string

func (b *bothType) MarshalText() ([]byte, error) { return []byte("txt"), nil }
func (b *bothType) UnmarshalText(t []byte) error { *b = bothType("txt"); return nil }

type bothConv struct{}

func (bothConv) Marshal(v reflect.Value) (string, error)   { return "conv", nil }
func (bothConv) Unmarshal(s string, f reflect.Value) error { f.SetString("conv"); return nil }

func TestConverter_TakesPrecedenceAtEveryDepth(t *testing.T) {
	type s struct {
		F  bothType            `form:"f"`
		Fs []bothType          `form:"fs"`
		M  map[string]bothType `form:"m"`
	}
	conv := bothConv{}
	typ := reflect.TypeOf(bothType(""))

	enc := NewEncoder(WithTextMarshalerSupport(true), WithCustomConverter(typ, conv))
	vals, err := enc.Marshal(s{F: "x", Fs: []bothType{"x"}, M: map[string]bothType{"k": "x"}})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, key := range []string{"f", "fs[0]", "m[k]"} {
		if got := vals.Get(key); got != "conv" {
			t.Errorf("encode %s = %q, want converter output \"conv\"", key, got)
		}
	}

	dec := NewDecoder(WithTextMarshalerSupport(true), WithCustomConverter(typ, conv))
	var out s
	if err := dec.Unmarshal(url.Values{"f": {"x"}, "fs": {"x"}, "m[k]": {"x"}}, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.F != "conv" {
		t.Errorf("decode leaf = %q, want converter output", out.F)
	}
	if out.Fs[0] != "conv" {
		t.Errorf("decode slice element = %q, want converter output (precedence flipped previously)", out.Fs[0])
	}
	if out.M["k"] != "conv" {
		t.Errorf("decode map value = %q, want converter output", out.M["k"])
	}
}

func TestConverter_Duration(t *testing.T) {
	// time.Duration implements fmt.Stringer but not TextMarshaler; test via parse
	enc := NewEncoder()

	type s struct {
		D time.Duration `form:"d"`
	}
	vals, err := enc.Marshal(s{D: 90 * time.Second})
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	if vals.Get("d") != "1m30s" {
		t.Errorf("d = %q", vals.Get("d"))
	}

	dec := NewDecoder()
	var out s
	if err := dec.Unmarshal(url.Values{"d": {"1m30s"}}, &out); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if out.D != 90*time.Second {
		t.Errorf("D = %v", out.D)
	}
}

func TestConverter_IP(t *testing.T) {
	enc := NewEncoder()

	type s struct {
		IP net.IP `form:"ip"`
	}
	vals, err := enc.Marshal(s{IP: net.ParseIP("192.168.1.10")})
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	if vals.Get("ip") != "192.168.1.10" {
		t.Errorf("ip = %q", vals.Get("ip"))
	}

	dec := NewDecoder()
	var out s
	if err := dec.Unmarshal(url.Values{"ip": {"10.0.0.1"}}, &out); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if !out.IP.Equal(net.ParseIP("10.0.0.1")) {
		t.Errorf("IP = %v", out.IP)
	}

	// empty value unmarshals to nil IP (not an error)
	var empty s
	if err := dec.Unmarshal(url.Values{"ip": {""}}, &empty); err != nil {
		t.Fatalf("unmarshal empty error: %v", err)
	}
	if empty.IP != nil {
		t.Errorf("IP = %v, want nil", empty.IP)
	}

	// invalid IP reports a parse error
	if err := dec.Unmarshal(url.Values{"ip": {"not-an-ip"}}, &s{}); err == nil {
		t.Error("expected error for invalid IP")
	}
}

func TestConverter_URL(t *testing.T) {
	enc := NewEncoder()

	type s struct {
		Link url.URL `form:"link"`
	}
	vals, err := enc.Marshal(s{Link: url.URL{Scheme: "https", Host: "example.com", Path: "/x"}})
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	if vals.Get("link") != "https://example.com/x" {
		t.Errorf("link = %q", vals.Get("link"))
	}

	dec := NewDecoder()
	var out s
	if err := dec.Unmarshal(url.Values{"link": {"https://example.com/x?q=1"}}, &out); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if out.Link.String() != "https://example.com/x?q=1" {
		t.Errorf("Link = %v", out.Link)
	}

	// empty value is left as-is (zero url.URL)
	var empty s
	if err := dec.Unmarshal(url.Values{"link": {""}}, &empty); err != nil {
		t.Fatalf("unmarshal empty error: %v", err)
	}
	if empty.Link.String() != "" {
		t.Errorf("Link = %v, want zero", empty.Link.String())
	}

	// invalid URL reports a parse error
	if err := dec.Unmarshal(url.Values{"link": {"://%"}}, &s{}); err == nil {
		t.Error("expected error for invalid URL")
	}
}
