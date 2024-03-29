package main

import (
	"log"
	"net/http"

	"github.com/guanke/papaya/api"
)

func main() {
	s := http.NewServeMux()

	s.HandleFunc("/healthcheck", api.Healthcheck)
	addr := ":3000"
	log.Fatal(http.ListenAndServe(addr, s))
}
