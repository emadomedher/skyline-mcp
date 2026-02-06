package main

import (
	"fmt"
	"io"
	"net/http"
	"time"
)

func main() {
	client := &http.Client{Timeout: 5 * time.Second}
	start := time.Now()

	resp, err := client.Get("http://localhost:9999/openapi/openapi.json")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("Read error: %v\n", err)
		return
	}

	fmt.Printf("Success! Got %d bytes in %v\n", len(body), time.Since(start))
}
