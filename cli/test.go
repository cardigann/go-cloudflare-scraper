package main

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"os"

	solver "github.com/cardigann/cf-challenge-solver"
)

func main() {
	c := http.Client{Transport: solver.NewTransport(http.DefaultTransport)}

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
