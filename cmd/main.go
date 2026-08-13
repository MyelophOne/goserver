package main

/* JUST AN EXAMPLE OF USAGE */

import (
	"github.com/myelophone/goserver"
)

func main() {
	httpPort := ":" + goserver.GetEnv("HTTP_PORT", "8080")
	s := goserver.NewServer(httpPort)

	s.Defaults()

	s.Run()
}
