package main

import (
	"log"
	"net/http"
)

func main() {
	c, err := loadConfig()
	if err != nil {
		log.Fatal(err)
	}
	server := &http.Server{Addr: ":8080", Handler: newApp(c).handler(), ReadHeaderTimeout: 5e9}
	log.Printf("reference app listening on :8080")
	log.Fatal(server.ListenAndServe())
}
