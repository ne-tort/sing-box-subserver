package box

import (
	"context"
	"testing"
)

func TestValidateMinimalConfig(t *testing.T) {
	eng := NewEngine(context.Background())
	raw := []byte(`{
		"log": {"level": "error"},
		"inbounds": [],
		"outbounds": [{"type": "direct", "tag": "direct"}]
	}`)
	if err := eng.Validate(context.Background(), raw); err != nil {
		t.Fatal(err)
	}
	if err := eng.Validate(context.Background(), []byte(`{`)); err == nil {
		t.Fatal("expected invalid json error")
	}
}
