package box

import (
	"context"
	"errors"
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

func TestRejectClashAPI(t *testing.T) {
	eng := NewEngine(context.Background())
	raw := []byte(`{
		"experimental": {"clash_api": {"external_controller": "127.0.0.1:9090"}},
		"outbounds": [{"type": "direct", "tag": "direct"}]
	}`)
	err := eng.Validate(context.Background(), raw)
	if err == nil || !errors.Is(err, ErrUnsupported) {
		t.Fatalf("want ErrUnsupported, got %v", err)
	}
}
