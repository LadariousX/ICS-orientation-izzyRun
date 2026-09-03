package main

import (
	"flag"
	"fmt"
	"log"

	"ICS-orientation-izzyRun/app"
)

func main() {
	port := flag.Int("port", 8080, "TCP port to listen on")
	flag.Parse()

	if err := app.Serve(fmt.Sprintf(":%d", *port)); err != nil {
		log.Fatal(err)
	}
}
