package main

import (
	"log"

	"apple-podcasts/internal/app"
)

func main() {
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
