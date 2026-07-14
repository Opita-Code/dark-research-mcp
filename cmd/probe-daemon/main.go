package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"time"
)

// One-shot probe: POST /v1/messages to dark-scrapper daemon, print raw body.
func main() {
	body := []byte(`{"model":"MiniMax-M3","max_tokens":8,"messages":[{"role":"user","content":"Reply pong"}]}`)
	req, _ := http.NewRequest("POST", "http://127.0.0.1:8901/v1/messages", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer ds-managed")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", "ds-managed")
	req.Header.Set("anthropic-version", "2023-06-01")

	httpc := &http.Client{Timeout: 30 * time.Second}
	resp, err := httpc.Do(req)
	if err != nil {
		fmt.Printf("ERROR: %v\n", err)
		return
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(resp.Body)
	fmt.Printf("STATUS: %d %s\n", resp.StatusCode, resp.Status)
	fmt.Printf("CONTENT-TYPE: %s\n", resp.Header.Get("content-type"))
	fmt.Printf("RAW BODY (%d bytes):\n%s\n", len(rb), string(rb))
}
