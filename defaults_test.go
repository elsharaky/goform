package goform

import (
	"errors"
	"net/url"
	"testing"
)

type defaultsReq struct {
	Name   string `form:"name"`
	Region string `form:"region,default:us-east"`
	Count  int    `form:"count,default:5"`
	Token  string `form:"token,required"`
	Flag   bool   `form:"flag,default:true"`
}

func TestDefaultTagAppliedWhenMissing(t *testing.T) {
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

func TestDefaultTagPreservesProvidedValue(t *testing.T) {
	var r defaultsReq
	vals := url.Values{"name": {"x"}, "region": {"eu-west"}, "token": {"abc"}}
	if err := NewDecoder().Unmarshal(vals, &r); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if r.Region != "eu-west" {
		t.Errorf("Region = %q, want provided eu-west", r.Region)
	}
}

func TestRequiredMissing(t *testing.T) {
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

func TestRequiredProvidedIsOK(t *testing.T) {
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

func TestRequiredNestedProvidedIsOK(t *testing.T) {
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

func TestRequiredNestedMissingStillErrors(t *testing.T) {
	var r nestedDefaultsReq
	err := NewDecoder().Unmarshal(url.Values{"ship_to.zip": {"1000"}}, &r)
	if err == nil {
		t.Fatal("expected error for missing nested required field")
	}
	if !errors.Is(err, ErrMissingRequired) {
		t.Errorf("expected ErrMissingRequired, got %v", err)
	}
}

func TestDefaultNestedPreservesProvidedValue(t *testing.T) {
	var r nestedDefaultsReq
	if err := NewDecoder().Unmarshal(url.Values{"ship_to.city": {"Lyon"}, "ship_to.zip": {"1000"}}, &r); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if r.ShipTo.Zip != "1000" {
		t.Errorf("Zip = %q, want provided 1000 (default clobbered it)", r.ShipTo.Zip)
	}
}

func TestDefaultNestedAppliedWhenAbsent(t *testing.T) {
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

func TestRequiredAltTagProvidedIsOK(t *testing.T) {
	var r altTagDefaultsReq
	if err := NewDecoder().Unmarshal(url.Values{"reg": {"eu"}}, &r); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if r.Region != "eu" {
		t.Errorf("Region = %q, want eu", r.Region)
	}
}

func TestDefaultAltTagPreservesProvidedValue(t *testing.T) {
	var r altTagDefaultsReq
	if err := NewDecoder().Unmarshal(url.Values{"reg": {"eu"}, "zn": {"eu-west"}}, &r); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if r.Zone != "eu-west" {
		t.Errorf("Zone = %q, want provided eu-west (default clobbered it)", r.Zone)
	}
}

func TestRequiredAltTagMissingStillErrors(t *testing.T) {
	var r altTagDefaultsReq
	err := NewDecoder().Unmarshal(url.Values{"zn": {"eu-west"}}, &r)
	if err == nil {
		t.Fatal("expected error for missing required field")
	}
	if !errors.Is(err, ErrMissingRequired) {
		t.Errorf("expected ErrMissingRequired, got %v", err)
	}
}
