package main

import (
	"io"
	"os"
	"reflect"
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

func TestParseFlagsCollectsRepeatedUpload(t *testing.T) {
	options, err := parseFlags([]string{"--upload", "a.txt", "--upload=dir"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(options.upload, []string{"a.txt", "dir"}) {
		t.Fatalf("upload = %v", options.upload)
	}
}
