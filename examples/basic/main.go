// Example: basic library usage of telconyx.
//
// Run with:
//
//	export TELCONYX_BOT_TOKEN=...
//	export TELCONYX_CHAT_ID=...
//	go run ./examples/basic
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/phalconyx/telconyx"
)

func main() {
	client, err := telconyx.NewClient(telconyx.Config{
		Token:  os.Getenv("TELCONYX_BOT_TOKEN"),
		ChatID: os.Getenv("TELCONYX_CHAT_ID"),
	})
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	ctx := context.Background()

	// 1) Upload a local file.
	src := "hello.txt"
	if err := os.WriteFile(src, []byte("hello telconyx\n"), 0o644); err != nil {
		log.Fatal(err)
	}
	defer os.Remove(src)

	result, err := client.UploadFile(ctx, src)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("=== upload ===")
	fmt.Println("URL:        ", result.Link())
	fmt.Println("FileID:     ", result.FileID)
	fmt.Println("MessageID:  ", result.MessageID)
	fmt.Println("Size:       ", result.Size)

	// 2) Save the URL wherever you like (your own DB, file, etc.).
	savedURL := result.Link()

	// 3) Later (could be days/months from now), download by URL.
	link, err := telconyx.ParseURL(savedURL)
	if err != nil {
		log.Fatal(err)
	}
	dest := "hello-downloaded.txt"
	defer os.Remove(dest)
	n, err := client.Download(ctx, link, dest)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("\n=== download ===")
	fmt.Printf("wrote %d bytes to %s\n", n, dest)
	fmt.Println("content:    ", mustReadFile(dest))
}

func mustReadFile(p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		return err.Error()
	}
	return string(b)
}
