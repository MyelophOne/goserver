package main

import (
	"fmt"
	"log"

	"github.com/myelophone/goserver/web/runtime"
)

func main() {
	config, err := runtime.UseRuntimeConfig()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Print(config.Runtime.Enabled)
}
