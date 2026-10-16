package ratelimit

// -raw parsing: accepts a pasted Burp/raw-HTTP request or a curl command
// (format auto-detected) and extracts method/url/host/headers/body from it.
// This lets you replay a request copied from Burp or browser devtools without
// hand-translating it into flags.

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// ParsedRequest holds everything extracted from a pasted raw HTTP request or
// curl command.
type ParsedRequest struct {
	Method   string
	URL      string // absolute URL, or a path (origin-form) for a Burp-style request
	Host     string // from the Host header / -H "Host:" / --url host; empty if none
	Headers  http.Header
	Body     []byte
	Insecure bool // curl -k / --insecure
}

// CurlCandidate summarizes one detected curl invocation, for the "multiple
// commands pasted" picker.
type CurlCandidate struct {
	Method string
	URL    string
}

// BuildHeaderFromList turns repeated "Key: Value" flag values into an http.Header.
func BuildHeaderFromList(list []string) http.Header {
	hdr := make(http.Header)
	for _, h := range list {
		colon := strings.Index(h, ":")
		if colon <= 0 {
			fmt.Fprintf(os.Stderr, "warning: invalid header (no colon): %q\n", h)
			continue
		}
		k := strings.TrimSpace(h[:colon])
		v := strings.TrimSpace(h[colon+1:])
		if k == "" {
			fmt.Fprintf(os.Stderr, "warning: empty header key in: %q\n", h)
			continue
		}
		hdr.Add(k, v)
	}
	return hdr
}

// MergeHeaders returns a new header set where entries from overrides replace
// same-key entries from base (the first override for a key clears base's values
// for that key; further overrides with the same key append).
func MergeHeaders(base, overrides http.Header) http.Header {
	out := make(http.Header)
	for k, vv := range base {
		for _, v := range vv {
			out.Add(k, v)
		}
	}
	replaced := map[string]bool{}
	for k, vv := range overrides {
		for _, v := range vv {
			if !replaced[k] {
				out.Del(k)
				replaced[k] = true
			}
			out.Add(k, v)
		}
	}
	return out
}

// LoadMaybeFile returns v as-is, or the contents of a file if v starts with "@"
// (the curl / -d convention).
func LoadMaybeFile(v string) ([]byte, error) {
	if strings.HasPrefix(v, "@") {
		return os.ReadFile(strings.TrimPrefix(v, "@"))
	}
	return []byte(v), nil
}

// LoadRawSource resolves where --raw content comes from: the system clipboard,
// stdin ("-"), a file ("@path"), or the literal flag value. It returns the text
// plus a short label describing the source (for diagnostics).
func LoadRawSource(rawFlag string, clipboard bool) (string, string, error) {
	if clipboard {
		content, err := readClipboard()
		if err != nil {
			return "", "", err
		}
		return content, "clipboard", nil
	}
	if rawFlag == "-" {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", "", fmt.Errorf("reading stdin: %w", err)
		}
		return string(b), "stdin", nil
	}
	if strings.HasPrefix(rawFlag, "@") {
		path := strings.TrimPrefix(rawFlag, "@")
		b, err := os.ReadFile(path)
		if err != nil {
			return "", "", fmt.Errorf("reading %q: %w", path, err)
		}
		return string(b), "file:" + path, nil
	}
	return rawFlag, "inline", nil
}

// readClipboard shells out to the platform's clipboard reader.
func readClipboard() (string, error) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("pbpaste")
	case "linux":
		switch {
		case commandExists("xclip"):
			cmd = exec.Command("xclip", "-selection", "clipboard", "-o")
		case commandExists("xsel"):
			cmd = exec.Command("xsel", "--clipboard", "--output")
		case commandExists("wl-paste"):
			cmd = exec.Command("wl-paste", "--no-newline")
		default:
			return "", fmt.Errorf("no clipboard tool found; install xclip, xsel, or wl-clipboard")
		}
	case "windows":
		cmd = exec.Command("powershell.exe", "-NoProfile", "-Command", "Get-Clipboard")
	default:
		return "", fmt.Errorf("clipboard reading not supported on %s; use --raw @file or --raw - instead", runtime.GOOS)
	}
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("running %s: %w", cmd.Path, err)
	}
	return string(out), nil
}

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// ParseRaw detects whether content is a curl command or a raw HTTP request and
// parses it accordingly. curlIndex selects which curl invocation to use when
// the input contains more than one; candidates lists all of them (for a
// caller-printed picker) and is empty for raw HTTP or a single curl command.
func ParseRaw(content string, curlIndex int) (parsed *ParsedRequest, format string, candidates []CurlCandidate, err error) {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	if strings.TrimSpace(content) == "" {
		return nil, "", nil, fmt.Errorf("--raw input is empty")
	}
	if detectRawFormat(content) == "curl" {
		pr, cands, perr := parseCurlCommand(content, curlIndex)
		return pr, "curl", cands, perr
	}
	pr, perr := parseRawHTTPRequest(content)
	return pr, "http", nil, perr
}

var knownHTTPMethods = map[string]bool{
	"GET": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true,
	"HEAD": true, "OPTIONS": true, "TRACE": true, "CONNECT": true,
}

// detectRawFormat sniffs whether pasted text is a curl command or a raw HTTP request.
func detectRawFormat(content string) string {
	t := strings.TrimSpace(content)
	if t == "" {
		return "http"
	}
	firstLine := strings.SplitN(t, "\n", 2)[0]
	fields := strings.Fields(firstLine)
	if len(fields) > 0 && knownHTTPMethods[strings.ToUpper(fields[0])] {
		return "http"
	}
	// Look for a "curl" token near the start (allowing leading env assignments
	// like FOO=bar curl ...).
	limit := len(fields)
	if limit > 6 {
		limit = 6
	}
	for i := 0; i < limit; i++ {
		if strings.EqualFold(fields[i], "curl") {
			return "curl"
		}
	}
	return "http"
}

// parseRawHTTPRequest parses a Burp-style raw HTTP request:
//
//	POST /api/v1/login HTTP/1.1
//	Host: example.com
//	Content-Type: application/json
//
//	{"user":"x"}
func parseRawHTTPRequest(raw string) (*ParsedRequest, error) {
	raw = strings.TrimLeft(raw, "\n")
	lines := strings.Split(raw, "\n")
	if len(lines) == 0 {
		return nil, fmt.Errorf("empty raw request")
	}

	reqLine := strings.TrimSpace(lines[0])
	parts := strings.Fields(reqLine)
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid request line: %q", reqLine)
	}

	pr := &ParsedRequest{
		Method:  strings.ToUpper(parts[0]),
		URL:     parts[1],
		Headers: make(http.Header),
	}

	i := 1
	for ; i < len(lines); i++ {
		line := lines[i]
		if strings.TrimSpace(line) == "" {
			i++
			break
		}
		colon := strings.Index(line, ":")
		if colon <= 0 {
			continue // skip malformed header line
		}
		k := strings.TrimSpace(line[:colon])
		v := strings.TrimSpace(line[colon+1:])
		if k == "" {
			continue
		}
		switch {
		case strings.EqualFold(k, "Host"):
			pr.Host = v
		case strings.EqualFold(k, "Content-Length"):
			// recomputed automatically from the body we actually send
		default:
			pr.Headers.Add(k, v)
		}
	}

	if i < len(lines) {
		body := strings.TrimRight(strings.Join(lines[i:], "\n"), "\n")
		if body != "" {
			pr.Body = []byte(body)
		}
	}

	return pr, nil
}

// parseCurlCommand parses a curl command line (Burp "Copy as curl" or browser
// devtools "Copy as cURL"), covering the flags that matter for rate-limit
// testing: -X, -H, -d/--data*, -b/--cookie, -A, -e/--referer, -u, -G, -k, --url.
//
// The input may contain several curl invocations separated by ";" (e.g. a
// devtools "copy all as cURL" export); each is parsed independently, index
// picks which one to return, and candidates lists all of them.
func parseCurlCommand(s string, index int) (*ParsedRequest, []CurlCandidate, error) {
	tokens, err := shellSplit(s)
	if err != nil {
		return nil, nil, err
	}

	segments := splitCurlInvocations(tokens)
	if len(segments) == 0 {
		return nil, nil, fmt.Errorf("no 'curl' command found in input")
	}

	var parsed []*ParsedRequest
	var candidates []CurlCandidate
	for _, seg := range segments {
		pr, perr := parseCurlArgs(seg)
		if perr != nil {
			continue // skip a malformed segment rather than failing the whole paste
		}
		parsed = append(parsed, pr)
		u := pr.URL
		if u == "" && pr.Host != "" {
			u = pr.Host
		}
		candidates = append(candidates, CurlCandidate{Method: pr.Method, URL: u})
	}
	if len(parsed) == 0 {
		return nil, nil, fmt.Errorf("found curl-like text but couldn't parse any of it")
	}
	if index < 0 || index >= len(parsed) {
		return nil, candidates, fmt.Errorf("--raw-index %d is out of range; input has %d curl command(s) (valid: 0-%d)", index, len(parsed), len(parsed)-1)
	}
	return parsed[index], candidates, nil
}

// splitCurlInvocations breaks a token stream into separate curl invocations,
// splitting on top-level ";" tokens and keeping only segments that contain a
// "curl" token (this also drops devtools' "# Unsupported URL scheme" comment
// segments). Each returned segment is the args that follow "curl".
func splitCurlInvocations(tokens []string) [][]string {
	var segments [][]string
	var cur []string
	flush := func() {
		idx := -1
		for i, t := range cur {
			if strings.EqualFold(t, "curl") {
				idx = i
				break
			}
		}
		if idx >= 0 {
			segments = append(segments, cur[idx+1:])
		}
		cur = nil
	}
	for _, t := range tokens {
		if t == ";" {
			flush()
			continue
		}
		cur = append(cur, t)
	}
	flush()
	return segments
}

// parseCurlArgs parses the flags of a single curl invocation (args after "curl").
func parseCurlArgs(args []string) (*ParsedRequest, error) {
	pr := &ParsedRequest{Headers: make(http.Header)}
	var dataParts [][]byte
	method := ""
	useGet := false
	explicitURL := ""
	sawForm := false

	takesValue := map[string]bool{
		"-X": true, "--request": true,
		"-H": true, "--header": true,
		"-d": true, "--data": true, "--data-raw": true, "--data-binary": true, "--data-ascii": true, "--data-urlencode": true,
		"-b": true, "--cookie": true,
		"-A": true, "--user-agent": true,
		"-e": true, "--referer": true,
		"-u": true, "--user": true,
		"--url": true,
		"-o": true, "--output": true,
		"-x": true, "--proxy": true,
		"-m": true, "--max-time": true,
		"--connect-timeout": true,
		"-F": true, "--form": true,
		"--cacert": true,
		"-c": true, "--cookie-jar": true,
	}

	for i := 0; i < len(args); i++ {
		a := args[i]
		switch a {
		case "-X", "--request":
			i++
			if i < len(args) {
				method = strings.ToUpper(args[i])
			}
		case "-H", "--header":
			i++
			if i < len(args) {
				colon := strings.Index(args[i], ":")
				if colon > 0 {
					k := strings.TrimSpace(args[i][:colon])
					v := strings.TrimSpace(args[i][colon+1:])
					if strings.EqualFold(k, "Host") {
						pr.Host = v
					} else if k != "" {
						pr.Headers.Add(k, v)
					}
				}
			}
		case "-d", "--data", "--data-raw", "--data-ascii", "--data-binary":
			i++
			if i < len(args) {
				b, err := LoadMaybeFile(args[i])
				if err != nil {
					return nil, fmt.Errorf("reading curl data argument: %w", err)
				}
				dataParts = append(dataParts, b)
			}
		case "--data-urlencode":
			i++
			if i < len(args) {
				dataParts = append(dataParts, []byte(args[i]))
			}
		case "-b", "--cookie":
			i++
			if i < len(args) {
				pr.Headers.Add("Cookie", args[i])
			}
		case "-A", "--user-agent":
			i++
			if i < len(args) {
				pr.Headers.Set("User-Agent", args[i])
			}
		case "-e", "--referer":
			i++
			if i < len(args) {
				pr.Headers.Set("Referer", args[i])
			}
		case "-u", "--user":
			i++
			if i < len(args) {
				enc := base64.StdEncoding.EncodeToString([]byte(args[i]))
				pr.Headers.Set("Authorization", "Basic "+enc)
			}
		case "--url":
			i++
			if i < len(args) {
				explicitURL = args[i]
			}
		case "-G", "--get":
			useGet = true
		case "-k", "--insecure":
			pr.Insecure = true
		case "-F", "--form":
			sawForm = true
			i++ // skip its value; multipart bodies aren't reconstructed
		default:
			if strings.HasPrefix(a, "-") {
				if takesValue[a] {
					i++ // unrecognized flag that takes a value; skip it
				}
				// otherwise a zero-arg flag we don't care about (-s, -v, -L, --compressed, -i, ...)
				continue
			}
			if explicitURL == "" {
				explicitURL = a
			}
		}
	}

	if sawForm {
		fmt.Fprintln(os.Stderr, "warning: curl -F/--form (multipart) is not reconstructed; body may be incomplete")
	}

	if len(dataParts) > 0 {
		body := bytes.Join(dataParts, []byte("&"))
		if useGet {
			sep := "?"
			if strings.Contains(explicitURL, "?") {
				sep = "&"
			}
			explicitURL = explicitURL + sep + string(body)
		} else {
			pr.Body = body
			if method == "" {
				method = "POST"
			}
		}
	}
	if method == "" {
		method = "GET"
	}

	pr.Method = method
	pr.URL = explicitURL
	return pr, nil
}

// shellSplit tokenizes a shell-like command line: handles single quotes, double
// quotes (with backslash escapes), $'...' ANSI-C quoting, bare backslash
// escapes, and backslash-newline line continuations.
func shellSplit(input string) ([]string, error) {
	input = strings.ReplaceAll(input, "\\\n", " ")

	var tokens []string
	var cur strings.Builder
	hasCur := false
	runes := []rune(input)
	n := len(runes)
	i := 0

	flush := func() {
		if hasCur {
			tokens = append(tokens, cur.String())
			cur.Reset()
			hasCur = false
		}
	}

	for i < n {
		c := runes[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			flush()
			i++
		case c == '\'':
			hasCur = true
			i++
			for i < n && runes[i] != '\'' {
				cur.WriteRune(runes[i])
				i++
			}
			if i >= n {
				return nil, fmt.Errorf("unterminated single quote")
			}
			i++
		case c == '$' && i+1 < n && runes[i+1] == '\'':
			hasCur = true
			i += 2
			for i < n && runes[i] != '\'' {
				if runes[i] == '\\' && i+1 < n {
					switch runes[i+1] {
					case 'n':
						cur.WriteRune('\n')
					case 't':
						cur.WriteRune('\t')
					case 'r':
						cur.WriteRune('\r')
					case '\\':
						cur.WriteRune('\\')
					case '\'':
						cur.WriteRune('\'')
					case '"':
						cur.WriteRune('"')
					default:
						cur.WriteRune(runes[i+1])
					}
					i += 2
				} else {
					cur.WriteRune(runes[i])
					i++
				}
			}
			if i >= n {
				return nil, fmt.Errorf("unterminated $'...' quote")
			}
			i++
		case c == '"':
			hasCur = true
			i++
			for i < n && runes[i] != '"' {
				if runes[i] == '\\' && i+1 < n && strings.ContainsRune(`"\$`+"`\n", runes[i+1]) {
					if runes[i+1] != '\n' {
						cur.WriteRune(runes[i+1])
					}
					i += 2
				} else {
					cur.WriteRune(runes[i])
					i++
				}
			}
			if i >= n {
				return nil, fmt.Errorf("unterminated double quote")
			}
			i++
		case c == '\\' && i+1 < n:
			hasCur = true
			cur.WriteRune(runes[i+1])
			i += 2
		default:
			hasCur = true
			cur.WriteRune(c)
			i++
		}
	}
	flush()
	return tokens, nil
}
