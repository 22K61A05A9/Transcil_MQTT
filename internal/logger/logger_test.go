package logger

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLogger(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "test.log")

	l, err := New(logPath)
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}

	l.Info("hello %s", "world")
	l.Error("something went wrong")

	if err := l.Close(); err != nil {
		t.Fatalf("failed to close logger: %v", err)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}

	content := string(data)

	if !strings.Contains(content, "[INFO]") {
		t.Error("expected INFO log")
	}

	if !strings.Contains(content, "[ERROR]") {
		t.Error("expected ERROR log")
	}

	if !strings.Contains(content, "hello world") {
		t.Error("expected info message")
	}
}
