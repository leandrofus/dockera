package server

import (
	"errors"
	"testing"
)

func spec() map[string]any {
	return map[string]any{
		"Mode":         map[string]any{"Replicated": map[string]any{"Replicas": float64(1)}},
		"TaskTemplate": map[string]any{"ContainerSpec": map[string]any{"Image": "nginx:1@sha256:abc"}},
	}
}

func TestApplyServiceAction(t *testing.T) {
	three := uint64(3)
	s := spec()
	if _, err := applyServiceAction(s, nil, "scale", &three, ""); err != nil {
		t.Fatal(err)
	}
	if got := s["Mode"].(map[string]any)["Replicated"].(map[string]any)["Replicas"]; got != uint64(3) {
		t.Fatalf("replicas = %v", got)
	}

	s = spec()
	applyServiceAction(s, nil, "restart", nil, "")
	applyServiceAction(s, nil, "restart", nil, "")
	if got := s["TaskTemplate"].(map[string]any)["ForceUpdate"]; got != float64(2) {
		t.Fatalf("ForceUpdate = %v", got)
	}

	s = spec()
	applyServiceAction(s, nil, "image", nil, "nginx:2")
	if got := serviceImage(s); got != "nginx:2" {
		t.Fatalf("image = %q", got)
	}

	global := map[string]any{"Mode": map[string]any{"Global": map[string]any{}}}
	if _, err := applyServiceAction(global, nil, "scale", &three, ""); err == nil {
		t.Fatal("scaling a global service must fail")
	}
	if _, err := applyServiceAction(spec(), nil, "rollback", nil, ""); err == nil {
		t.Fatal("rollback without a previous spec must fail")
	}
	if rb, err := applyServiceAction(spec(), spec(), "rollback", nil, ""); err != nil || !rb {
		t.Fatalf("rollback = %v, %v", rb, err)
	}
}

func TestRetrySequence(t *testing.T) {
	n := 0
	err := retrySequence(func() error {
		if n++; n < 3 {
			return errors.New("rpc error: update out of sequence")
		}
		return nil
	})
	if err != nil || n != 3 {
		t.Fatalf("err=%v after %d tries", err, n)
	}
	n = 0
	retrySequence(func() error { n++; return errors.New("other") })
	if n != 1 {
		t.Fatalf("non-sequence errors must not be retried (%d tries)", n)
	}
}
