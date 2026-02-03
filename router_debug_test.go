package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

func TestRouterDebugMain(t *testing.T) {
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	os.Stdout = w

	defer func() {
		_ = w.Close()
		os.Stdout = oldStdout
	}()

	var buf bytes.Buffer
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(&buf, r)
		close(done)
	}()

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("main panicked: %v", r)
		}
	}()

	main()
	_ = w.Close()
	<-done

	output := buf.String()
	if !strings.Contains(output, "selected") {
		t.Fatalf("expected output to contain selection result")
	}
}
