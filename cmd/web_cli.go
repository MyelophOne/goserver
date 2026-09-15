//go:build webcli

package main

import (
	"log"
	"os"

	"github.com/myelophone/goserver"
)

func main() {
	if err := goserver.RunWebCLI(os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}
