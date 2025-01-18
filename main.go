package main

import (
	"gorsync/cmd"
	"log"
)

func main() {
	if err := cmd.Execute(); err != nil {
		log.Fatalf("Error: %v", err)
	}
}
