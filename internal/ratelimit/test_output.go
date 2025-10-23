package ratelimit

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func DoTestOutput(cfg Config) {
	fmt.Println(">>> Sending test request...")
	client := newClient()
	req, err := buildRequest(cfg)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println("Request error:", err)
		return
	}
	defer resp.Body.Close()

	fmt.Printf("<<< Response: %d %s\n", resp.StatusCode, resp.Status)
	fmt.Println("---- Headers ----")
	for k, v := range resp.Header {
		fmt.Printf("%s: %s\n", k, strings.Join(v, ", "))
	}

	fmt.Println("---- Body ----")
	var reader io.Reader = resp.Body
	if strings.Contains(resp.Header.Get("Content-Encoding"), "gzip") {
		gz, err := gzip.NewReader(resp.Body)
		if err == nil {
			defer gz.Close()
			reader = gz
		}
	}
	data, _ := io.ReadAll(reader)
	fmt.Println(string(bytes.TrimSpace(data)))
}
