package main

import (
	"log"
	"os"
)

var stdLog = log.Default()

func setupLogging(logFile string) {
	flags := log.Ldate | log.Ltime | log.Lmicroseconds
	if logFile != "" {
		f, err := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err == nil {
			stdLog.SetOutput(f)
		} else {
			logError("не удалось открыть лог-файл %s: %v", logFile, err)
		}
	}
	stdLog.SetFlags(flags)
}

func logInfo(format string, args ...any) {
	stdLog.Printf("INFO    "+format, args...)
}

func logWarn(format string, args ...any) {
	stdLog.Printf("WARNING "+format, args...)
}

func logError(format string, args ...any) {
	stdLog.Printf("ERROR   "+format, args...)
}

func logFatal(format string, args ...any) {
	stdLog.Printf("FATAL   "+format, args...)
	os.Exit(1)
}
