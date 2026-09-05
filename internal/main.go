package main

import (
	"log"
	"internal/server"
	"internal/store"
)

func main() {
	myStore := store.NewStore();

	serv := server.NewServer(":13203", myStore);

	if err := serv.Start(); err != nil {
		log.Fatalf("Fatal server crash: %v", err);
	}
}
