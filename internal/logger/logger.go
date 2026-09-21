package logger

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
)

type Logger struct {
	infoLogger  *log.Logger
	errorLogger *log.Logger

	file *os.File
}

func New(logPath string) (*Logger, error) {
	dir := filepath.Dir(logPath)

	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create log directory: %w", err)
	}

	file, err := os.OpenFile(
		logPath,
		os.O_CREATE|os.O_APPEND|os.O_WRONLY,
		0644,
	)
	if err != nil {
		return nil, fmt.Errorf("open log file: %w", err)
	}

	output := io.MultiWriter(
		os.Stdout,
		file,
	)

	return &Logger{
		infoLogger: log.New(
			output,
			"[INFO] ",
			log.Ldate|log.Ltime,
		),

		errorLogger: log.New(
			output,
			"[ERROR] ",
			log.Ldate|log.Ltime,
		),

		file: file,
	}, nil
}

func (l *Logger) Info(format string, args ...interface{}) {
	l.infoLogger.Printf(format, args...)
}

func (l *Logger) Error(format string, args ...interface{}) {
	l.errorLogger.Printf(format, args...)
}

func (l *Logger) Close() error {
	if l.file == nil {
		return nil
	}

	return l.file.Close()
}
