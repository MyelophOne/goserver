package goserver

import "testing"

func TestDebugChunks(t *testing.T) {
	a, err := NewWebApp()
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range a.sourceChunks {
		t.Logf("page=%q ids=%d", k, len(v))
	}
}
