package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	scraper "github.com/cardigann/go-cloudflare-scraper"
)

func makeRequest(c *http.Client, url string) {
	t := time.Now()

	log.Printf("Requesting %s", url)
	resp, err := c.Get(url)
	if err != nil {
		log.Fatal(err)
	}

	body, _ := ioutil.ReadAll(resp.Body)
	log.Printf("Fetched %s in %s, %d bytes (status %d)",
		url, time.Now().Sub(t), len(body), resp.StatusCode)

	if resp.StatusCode != http.StatusOK {
		log.Fatal("Invalid response code")
	}
}

func main() {
	var parallel = flag.Int("parallel", 1, "Number of parallel requests to run")
	flag.Parse()

	if len(os.Args) < 2 {
		fmt.Printf("usage: %s [url]\n", os.Args[0])
		os.Exit(1)
	}

	t, err := scraper.NewTransport(http.DefaultTransport)
	if err != nil {
		log.Fatal(err)
	}

	client := &http.Client{Transport: t}
	wg := sync.WaitGroup{}
	wg.Add(*parallel)

	for i := 0; i < *parallel; i++ {
		go func() {
			makeRequest(client, flag.Arg(0))
			wg.Done()
		}()
	}

	wg.Wait()
}
