package ratelimit

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
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
	fmt.Printf("---- HTTP Version %s ----\n", cfg.HTTPVersion)
	for k, v := range resp.Header {
		fmt.Printf("%s: %s\n", k, strings.Join(v, ", "))
	}

	fmt.Println("---- Body ----")
	var reader io.Reader = resp.Body
	if strings.Contains(resp.Header.Get("Content-Encoding"), "gzip") {
		gz, err := gzip.NewReader(resp.Body)
		if err == nil {
			fmt.Fprintf(os.Stderr, "Failed to create gzip reader: %v\n", err)
			return
		}
		defer gz.Close()
		reader = gz
	} else {
		reader = resp.Body
	}

	bodyBytes, err := io.ReadAll(reader)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to read response body: %v\n", err)
		return
	}

	// try to pretty print JSON if applicable
	var pretty bytes.Buffer
	if json.Indent(&pretty, bodyBytes, "", "  ") == nil {
		fmt.Println(pretty.String())
	} else {
		fmt.Println(string(bodyBytes))
	}
}
