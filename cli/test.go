package main

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"os"

	scraper "github.com/cardigann/go-cloudflare-scrape"
)

func main() {
	c := http.Client{Transport: scraper.NewTransport(http.DefaultTransport)}

	if len(os.Args) < 2 {
		fmt.Printf("usage: %s [url]\n", os.Args[0])
		os.Exit(1)
	}

	resp, err := c.Get(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}

	b, err := httputil.DumpResponse(resp, true)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("%s", b)
}
