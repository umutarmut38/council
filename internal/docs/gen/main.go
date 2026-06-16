// Command gen regenerates the generated regions of the Markdown docs. It is the
// target of the package's go:generate directive (`go generate ./...`).
package main

import (
	"log"

	"github.com/umutarmut38/council/internal/docs"
)

func main() {
	if err := docs.Write(); err != nil {
		log.Fatal(err)
	}
}
