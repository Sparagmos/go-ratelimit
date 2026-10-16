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

// DoTestOutput sends a single request described by cfg and pretty-prints the
// response (status, headers, and a JSON-indented body when possible). It never
// generates load — it is the safe smoke test before a real run.
func DoTestOutput(cfg Config) {
	client := newClient(cfg.Timeout, cfg.Insecure)

	req, err := buildRequest(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error building request: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(">>> Sending test request...")
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println("<<< Request failed")
		fmt.Printf("    Method: %s\n", cfg.Method)
		fmt.Printf("    Target: %s\n", cfg.URL)
		fmt.Printf("    Reason: %s\n", classifyRequestError(err))
		os.Exit(1)
	}
	defer resp.Body.Close()

	fmt.Printf("<<< Response: %s\n", resp.Status)
	fmt.Printf("---- HTTP Version %s ----\n", resp.Proto)
	fmt.Println("---- Headers ----")
	for k, v := range resp.Header {
		fmt.Printf("%s: %s\n", k, strings.Join(v, ", "))
	}

	fmt.Println("---- Body ----")
	var reader io.Reader = resp.Body
	if strings.Contains(resp.Header.Get("Content-Encoding"), "gzip") {
		gz, gzErr := gzip.NewReader(resp.Body)
		if gzErr != nil {
			fmt.Fprintf(os.Stderr, "Failed to create gzip reader: %v\n", gzErr)
			return
		}
		defer gz.Close()
		reader = gz
	}

	b, err := io.ReadAll(reader)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to read response body: %v\n", err)
		return
	}

	// Pretty-print JSON when the body parses as JSON; otherwise print as-is.
	var pretty bytes.Buffer
	if json.Indent(&pretty, b, "", "  ") == nil {
		fmt.Println(pretty.String())
	} else {
		fmt.Println(string(b))
	}
}
