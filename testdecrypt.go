//go:build ignore
// +build ignore

package main

import (
	"encoding/json"
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run testdecrypt.go <ncm file>")
		return
	}

	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Printf("read error: %v\n", err)
		return
	}

	result, audioData, err := decryptNCM(data, os.Args[1])
	if err != nil {
		fmt.Printf("decrypt error: %v\n", err)
		return
	}

	b, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println("=== Result ===")
	fmt.Println(string(b))
	fmt.Printf("Cover size: %d bytes\n", len(result.CoverData))
	fmt.Printf("Audio size: %d bytes\n", len(audioData))
	fmt.Printf("Audio format: %s\n", result.Format)
	fmt.Printf("Title: %q\n", result.Title)
	fmt.Printf("Artist: %q\n", result.Artist)
	fmt.Printf("Album: %q\n", result.Album)
}
