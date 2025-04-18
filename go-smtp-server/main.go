package main

import (
	"log"
)

// main is the entry point of the application
// It starts the SMTP server on port 2525 and blocks indefinitely
func main() {
	launchScheduler() // <‑‑ start the retry delivery goroutine

	if _, _, err := Start(":2525"); err != nil {
		log.Fatal(err)
	}
	select {} // Block forever (until process is terminated)
}
