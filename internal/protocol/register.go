// Package protocol implements the Agent↔Master shared types for the
// out-of-band registration flow (DESIGN.md §4.1).
//
// Registration is a one-time, out-of-band event: the Agent generates a
// registration message, the user forwards it to the Telegram bot, and
// Master stores the Agent's UUID in its whitelist.
package protocol

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

// RegPrefix is the prefix for registration messages.
const RegPrefix = "SENTINEL-REG:"

// MaxUUIDLen is the maximum length of a UUID string.
const MaxUUIDLen = 36

// MaxAliasLen is the maximum length of an alias.
const MaxAliasLen = 20

// RegVersion is the current registration protocol version.
const RegVersion = 1

// Registration is the registration payload exchanged out-of-band.
type Registration struct {
	Version int    `json:"v"`
	Region  string `json:"region"`
	Node    string `json:"node"`
	Alias   string `json:"alias"`
	UUID    string `json:"uuid"`
	OTA     bool   `json:"ota"`
}

// Encode serialises a Registration to the wire format:
//
//	SENTINEL-REG:<base64(json)>
func (r *Registration) Encode() (string, error) {
	if r.Version == 0 {
		r.Version = RegVersion
	}
	data, err := json.Marshal(r)
	if err != nil {
		return "", fmt.Errorf("protocol: marshal registration: %w", err)
	}
	return RegPrefix + base64.StdEncoding.EncodeToString(data), nil
}

// Decode parses a registration message from the wire format.
// It validates the prefix, base64, JSON structure, and field constraints.
func Decode(msg string) (*Registration, error) {
	msg = strings.TrimSpace(msg)
	if !strings.HasPrefix(msg, RegPrefix) {
		return nil, fmt.Errorf("protocol: missing %q prefix", RegPrefix)
	}
	b64 := strings.TrimPrefix(msg, RegPrefix)
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, fmt.Errorf("protocol: base64 decode: %w", err)
	}
	if len(raw) > 64*1024 {
		return nil, fmt.Errorf("protocol: payload exceeds 64 KiB")
	}

	var reg Registration
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&reg); err != nil {
		return nil, fmt.Errorf("protocol: json decode: %w", err)
	}

	if err := reg.validate(); err != nil {
		return nil, err
	}
	return &reg, nil
}

func (r *Registration) validate() error {
	if r.Version != RegVersion {
		return fmt.Errorf("protocol: unsupported version %d (want %d)", r.Version, RegVersion)
	}
	if r.UUID == "" || len(r.UUID) > MaxUUIDLen {
		return fmt.Errorf("protocol: uuid length %d out of range", len(r.UUID))
	}
	// UUID must look like a v4 UUID: 8-4-4-4-12 hex digits.
	if !isValidUUID(r.UUID) {
		return fmt.Errorf("protocol: invalid uuid format")
	}
	if r.Region == "" {
		return fmt.Errorf("protocol: empty region")
	}
	if len(r.Region) > 10 {
		return fmt.Errorf("protocol: region too long")
	}
	if r.Node == "" {
		return fmt.Errorf("protocol: empty node name")
	}
	if len(r.Node) > 64 {
		return fmt.Errorf("protocol: node name too long")
	}
	if len([]rune(r.Alias)) > MaxAliasLen {
		return fmt.Errorf("protocol: alias too long (%d runes)", len([]rune(r.Alias)))
	}
	return nil
}

func isValidUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
				return false
			}
		}
	}
	return true
}
