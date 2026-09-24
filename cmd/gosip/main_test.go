package main

import "testing"

func TestDoSetIPMissingArgsDoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("doSetIP panicked: %v", r)
		}
	}()

	if err := doSetIP(nil); err == nil {
		t.Fatal("expected an error for missing required setip arguments")
	}
}
