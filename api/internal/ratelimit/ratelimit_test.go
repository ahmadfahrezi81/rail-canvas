package ratelimit

import (
	"testing"
	"time"
)

func TestBurstThenRefuse(t *testing.T) {
	l := New(time.Minute, 5)
	for i := range 5 {
		if !l.Allow("a") {
			t.Fatalf("attempt %d refused, want allowed", i+1)
		}
	}
	if l.Allow("a") {
		t.Error("6th attempt allowed, want refused")
	}
	if !l.Allow("b") {
		t.Error("another key was refused")
	}
}
