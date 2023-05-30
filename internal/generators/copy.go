//go:build tools
// +build tools

package main

import (
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/ubuntu/zsys/internal/generators"
)

func main() {
	if len(os.Args) != 4 {
		log.Fatal("Expect 3 arguments: <source> <dest> <root_share>")
	}
	root := generators.DestDirectory(os.Args[3])
	dir := filepath.Join(root, os.Args[2])

	if err := os.MkdirAll(dir, 0755); err != nil {
		log.Fatalf("Couldn't create dest directory: %v", err)
	}

	from, err := os.Open(os.Args[1])
	if err != nil {
		log.Fatalf("Couldn't open source file: %v", err)
	}
	defer from.Close()

	dest, err := os.OpenFile(filepath.Join(dir, filepath.Base(os.Args[1])), os.O_RDWR|os.O_CREATE, 0666)
	if err != nil {
		log.Fatal("Couldn't open dest file: %v", err)
	}
	defer dest.Close()

	_, err = io.Copy(dest, from)
	if err != nil {
		log.Fatalf("Copy failed: %v", err)
	}
}
