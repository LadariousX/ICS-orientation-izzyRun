package main

import (
	"fmt"
	"log"
	"os"

	"ICS-tabling-demo/app"

	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load()
	// override port in .env for perm. server deploy. use 8080 for local and RPi hosting
	port, exists := os.LookupEnv("PORT")
	if !exists {
		port = "8080"
	}
	if err := app.Serve(fmt.Sprintf(":%s", port), ""); err != nil {
		log.Fatal(err)
	}
}
