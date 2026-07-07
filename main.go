package main

import (
	"log"
	"net/http"

	"ICS-tabling-demo/app"
)

const addr = ":8080"

func main() {
	mux := http.NewServeMux()
	app.Register(mux)

	log.Printf("listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}
