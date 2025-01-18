package utils

import (
	"log"
	"os"
)

var (
	InfoLogger  *log.Logger
	ErrorLogger *log.Logger
)

func InitLogger() {
	InfoLogger = log.New(os.Stdout, "INFO: ", log.Ldate|log.Ltime|log.Lshortfile)
	ErrorLogger = log.New(os.Stderr, "ERROR: ", log.Ldate|log.Ltime|log.Lshortfile)
}

func Info(msg string, args ...interface{}) {
	InfoLogger.Printf(msg, args...)
}

func Error(msg string, args ...interface{}) {
	ErrorLogger.Printf(msg, args...)
}
