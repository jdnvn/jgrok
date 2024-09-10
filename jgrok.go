package main

import (
	"log"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatal("You must specify either 'server' or 'client'")
	}

	switch os.Args[1] {
	case "server":
		startServer()
	case "client":
		startClient()
	default:
		log.Fatalf("unknown command '%s'. you must specify either 'server' or 'client'", os.Args[1])
	}
}
