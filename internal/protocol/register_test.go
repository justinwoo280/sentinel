package protocol

import (
	"encoding/base64"
	"strings"
	"testing"
)

func validReg() Registration {
	return Registration{
		Version: 1,
		Region:  "JP",
		Node:    "tokyo-a1b2",
		Alias:   "东京-1",
		UUID:    "11111111-2222-3333-4444-555555555555",
		OTA:     true,
	}
}

func TestEncodeDecode(t *testing.T) {
	reg := validReg()
	enc, err := reg.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(enc, RegPrefix) {
		t.Fatalf("missing prefix: %s", enc)
	}

	got, err := Decode(enc)
	if err != nil {
		t.Fatal(err)
	}
	if got.UUID != reg.UUID {
		t.Errorf("uuid: got %q, want %q", got.UUID, reg.UUID)
	}
	if got.Region != reg.Region {
		t.Errorf("region: got %q, want %q", got.Region, reg.Region)
	}
	if got.Alias != reg.Alias {
		t.Errorf("alias: got %q, want %q", got.Alias, reg.Alias)
	}
	if got.OTA != reg.OTA {
		t.Errorf("ota: got %v, want %v", got.OTA, reg.OTA)
	}
}

func TestDecodeNoPrefix(t *testing.T) {
	_, err := Decode("not-a-reg-message")
	if err == nil {
		t.Fatal("expected error for missing prefix")
	}
}

func TestDecodeBadBase64(t *testing.T) {
	_, err := Decode(RegPrefix + "!!!not-base64!!!")
	if err == nil {
		t.Fatal("expected error for invalid base64")
	}
}

func TestDecodeUnknownField(t *testing.T) {
	// Craft a JSON with an unknown field.
	raw := `{"v":1,"region":"JP","node":"tokyo-a1b2","alias":"东京-1","uuid":"11111111-2222-3333-4444-555555555555","ota":true,"evil":"inject"}`
	enc := RegPrefix + base64Encode(raw)
	_, err := Decode(enc)
	if err == nil {
		t.Fatal("expected error for unknown field")
	}
}

func TestDecodeInvalidUUID(t *testing.T) {
	reg := validReg()
	reg.UUID = "not-a-uuid"
	enc, _ := reg.Encode()
	_, err := Decode(enc)
	if err == nil {
		t.Fatal("expected error for invalid UUID")
	}
}

func TestDecodeEmptyRegion(t *testing.T) {
	reg := validReg()
	reg.Region = ""
	enc, _ := reg.Encode()
	_, err := Decode(enc)
	if err == nil {
		t.Fatal("expected error for empty region")
	}
}

func TestDecodeEmptyNode(t *testing.T) {
	reg := validReg()
	reg.Node = ""
	enc, _ := reg.Encode()
	_, err := Decode(enc)
	if err == nil {
		t.Fatal("expected error for empty node")
	}
}

func TestDecodeLongAlias(t *testing.T) {
	reg := validReg()
	reg.Alias = strings.Repeat("X", MaxAliasLen+1)
	enc, _ := reg.Encode()
	_, err := Decode(enc)
	if err == nil {
		t.Fatal("expected error for long alias")
	}
}

func TestDecodeWrongVersion(t *testing.T) {
	reg := validReg()
	reg.Version = 99
	enc, _ := reg.Encode()
	_, err := Decode(enc)
	if err == nil {
		t.Fatal("expected error for wrong version")
	}
}

func TestEncodeAutoVersion(t *testing.T) {
	reg := validReg()
	reg.Version = 0
	enc, err := reg.Encode()
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(enc)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != RegVersion {
		t.Fatalf("auto-version: got %d, want %d", got.Version, RegVersion)
	}
}

func TestIsValidUUID(t *testing.T) {
	valid := []string{
		"11111111-2222-3333-4444-555555555555",
		"00000000-0000-0000-0000-000000000000",
		"abcdefab-cdef-abcd-efab-abcdefabcdef",
	}
	for _, s := range valid {
		if !isValidUUID(s) {
			t.Errorf("isValidUUID(%q) = false, want true", s)
		}
	}
	invalid := []string{
		"",
		"not-a-uuid",
		"11111111-2222-3333-4444",
		"11111111-2222-3333-4444-555555555555-extra",
		"gggggggg-2222-3333-4444-555555555555",
		"11111111x2222-3333-4444-555555555555",
	}
	for _, s := range invalid {
		if isValidUUID(s) {
			t.Errorf("isValidUUID(%q) = true, want false", s)
		}
	}
}

func base64Encode(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}

// FuzzDecode ensures the registration decoder never panics on arbitrary
// input, and that any successfully decoded Registration satisfies all
// field constraints (SR-5: parser hardening).
func FuzzDecode(f *testing.F) {
	valid, _ := (&Registration{
		Version: 1, Region: "JP", Node: "n", Alias: "a",
		UUID: "11111111-2222-3333-4444-555555555555", OTA: true,
	}).Encode()
	f.Add(valid)
	f.Add("")
	f.Add("SENTINEL-REG:")
	f.Add("SENTINEL-REG:!!!!")
	f.Add("SENTINEL-REG:" + base64Encode(`{"v":1}`))
	f.Add("SENTINEL-REG:" + base64Encode(`{"v":1,"uuid":"bad","region":"JP","node":"n"}`))
	f.Add("SENTINEL-REG:" + base64Encode(`{"v":1,"evil":true}`))
	f.Add("no-prefix")
	f.Add("SENTINEL-REG:" + base64Encode(string(make([]byte, 70000))))

	f.Fuzz(func(t *testing.T, msg string) {
		reg, err := Decode(msg) // must never panic
		if err != nil {
			return
		}
		// Invariants on successful decode.
		if reg.Version != RegVersion {
			t.Fatalf("decoded version %d != %d", reg.Version, RegVersion)
		}
		if !isValidUUID(reg.UUID) {
			t.Fatalf("decoded invalid uuid: %q", reg.UUID)
		}
		if len([]rune(reg.Alias)) > MaxAliasLen {
			t.Fatalf("decoded alias too long: %q", reg.Alias)
		}
		if reg.Region == "" || len(reg.Region) > 10 {
			t.Fatalf("decoded bad region: %q", reg.Region)
		}
		if reg.Node == "" || len(reg.Node) > 64 {
			t.Fatalf("decoded bad node: %q", reg.Node)
		}
	})
}
