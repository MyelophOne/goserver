package goserver

import (
	"bytes"
	"log"
	"strings"
	"testing"
)

func TestLevelWriterFiltersMessagesBelowMinimum(t *testing.T) {
	var output bytes.Buffer
	logger := log.New(levelWriter{Target: &output, Minimum: LogWarn}, "", 0)
	logger.Print("INFO: hidden")
	logger.Print("WARN: visible")
	logger.Print("ERROR: visible")

	got := output.String()
	if strings.Contains(got, "hidden") || !strings.Contains(got, "WARN: visible") || !strings.Contains(got, "ERROR: visible") {
		t.Fatalf("unexpected filtered log output: %q", got)
	}
}

func TestFormatLogClientMakesLoopbackExplicit(t *testing.T) {
	if got := formatLogClient("::1", LogClientIPMasked); got != "localhost" {
		t.Fatalf("formatLogClient(::1) = %q", got)
	}
	if got := formatLogClient("198.51.100.42", LogClientIPMasked); got != "198.51.100.x" {
		t.Fatalf("masked IPv4 = %q", got)
	}
}
