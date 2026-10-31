package main

import (
	"log"
	"net/http"
	"os"

	"example.com/computer-network/project/apps/api/internal/httpapi"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	addr := ":" + port
	log.Printf("API listening on http://localhost:%s", port)
	if err := http.ListenAndServe(addr, httpapi.NewHandler()); err != nil {
		log.Fatal(err)
	}
}
