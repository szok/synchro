package main

import (
	"os"
	"testing"
)

func TestParseFlagsAcceptsTest(t *testing.T) {
	errOut, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	defer errOut.Close()

	options, err := parseFlags([]string{"--test"}, errOut)
	if err != nil {
		t.Fatal(err)
	}
	if !options.test {
		t.Fatal("test flag was not set")
	}
}
