package agent

import (
	"reflect"
	"testing"

	"github.com/justinwoo280/sentinel/internal/agent/modules/quality"
	"github.com/justinwoo280/sentinel/internal/config"
)

// TestQualityAPIKeysInSync guards against drift between the config mirror
// struct (config.QualityAPIKeys) and the quality module's APIKeys struct.
// If someone adds a key to one but not the other, this fails.
func TestQualityAPIKeysInSync(t *testing.T) {
	ct := reflect.TypeOf(config.QualityAPIKeys{})
	qt := reflect.TypeOf(quality.APIKeys{})

	if ct.NumField() != qt.NumField() {
		t.Fatalf("field count mismatch: config has %d, quality has %d",
			ct.NumField(), qt.NumField())
	}
	for i := 0; i < ct.NumField(); i++ {
		cf := ct.Field(i)
		qf := qt.Field(i)
		if cf.Name != qf.Name {
			t.Errorf("field %d name mismatch: config=%q quality=%q", i, cf.Name, qf.Name)
		}
		if cf.Type != qf.Type {
			t.Errorf("field %q type mismatch: config=%v quality=%v", cf.Name, cf.Type, qf.Type)
		}
		if cf.Tag.Get("yaml") != qf.Tag.Get("yaml") {
			t.Errorf("field %q yaml tag mismatch: config=%q quality=%q",
				cf.Name, cf.Tag.Get("yaml"), qf.Tag.Get("yaml"))
		}
	}
}
