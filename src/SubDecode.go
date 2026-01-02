package main

import (
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.design/x/clipboard"
)

var userAgents = map[string]string{
	"chrome":  "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/120.0.0.0 Safari/537.36",
	"firefox": "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:121.0) Gecko/20100101 Firefox/121.0",
	"edge":    "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Edg/120.0.0.0",
	"curl":    "curl/8.5.0",
}

func main() {
	link := flag.String("link", "", "subscription link or local file (required)")
	flag.StringVar(link, "l", "", "subscription link or local file (short)")

	proxy := flag.String("proxy", "", "proxy URL (optional)")
	flag.StringVar(proxy, "p", "", "proxy URL (short)")

	ua := flag.String("ua", "chrome", "user agent: chrome|firefox|edge|curl")

	help := flag.Bool("help", false, "show help")
	flag.BoolVar(help, "h", false, "show help (short)")

	flag.Parse()

	if *help || len(os.Args) == 1 {
		showHelp()
		return
	}

	if *link == "" {
		fmt.Println("❌ No subscription link or file provided!")
		showHelp()
		return
	}

	var newLines []string

	if isURL(*link) {
		client, err := buildClient(*proxy)
		if err != nil {
			fmt.Println("❌ Proxy error:", err)
			return
		}

		req, err := http.NewRequest("GET", *link, nil)
		if err != nil {
			fmt.Println("❌ Invalid URL:", err)
			return
		}

		agent := userAgents[*ua]
		if agent == "" {
			agent = userAgents["chrome"]
		}
		req.Header.Set("User-Agent", agent)

		resp, err := client.Do(req)
		if err != nil {
			printNetError(err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			fmt.Printf("❌ HTTP error: %s (%d)\n", resp.Status, resp.StatusCode)
			return
		}

		body, _ := io.ReadAll(resp.Body)
		content := string(body)

		decoded, err := decodeBase64(content)
		if err != nil {
			newLines = normalize(content)
			fmt.Println("ℹ Plain text subscription detected from URL")
		} else {
			newLines = normalize(decoded)
			fmt.Println("ℹ Base64 subscription detected from URL")
		}

	} else if fileExists(*link) {
		content, err := os.ReadFile(*link)
		if err != nil {
			fmt.Println("❌ Failed to read file:", err)
			return
		}
		newLines = normalize(string(content))
		fmt.Println("ℹ Read configs from local file:", *link)
	} else {
		fmt.Println("❌ Input is neither a valid URL nor an existing file")
		return
	}

	outDir := "output"
	_ = os.MkdirAll(outDir, 0755)

	filename := sanitizeFileName(*link)
	outFile := filepath.Join(outDir, filename+".txt")

	oldLines := loadExisting(outFile)
	added, unchanged, removed := diff(oldLines, newLines)

	if added == 0 && removed == 0 {
		fmt.Println("ℹ No updates detected, file is already up to date.")
	} else {
		final := strings.Join(newLines, "\n")
		err := os.WriteFile(outFile, []byte(final), 0644)
		if err != nil {
			fmt.Println("❌ Failed to write output file:", err)
			return
		}

		if err := clipboard.Init(); err == nil {
			clipboard.Write(clipboard.FmtText, []byte(final))
		}

		fmt.Println("✅ Subscription updated!")
		fmt.Printf("➕ Added: %d\n", added)
		fmt.Printf("✔ Unchanged: %d\n", unchanged)
		fmt.Printf("➖ Removed: %d\n", removed)
		fmt.Println("📋 Copied to clipboard")
		fmt.Printf("📄 Saved to %s\n", outFile)
	}
}

func showHelp() {
	fmt.Println(`
================= SubDecode =================

A simple tool to fetch, decode, deduplicate, and copy subscription links.

Usage:
  SubDecode [flags]

Flags:
  -l, --link   <URL|file> Subscription link (Base64/plain) or local text file (required)
  -p, --proxy  <URL>      Optional HTTP/HTTPS proxy (e.g., http://127.0.0.1:10809)
  -ua          <agent>    User-Agent: chrome | firefox | edge | curl (default: chrome)
  -h, --help              Show this help page

By Diamond (https://github.com/Diamond0098)
`)
}

func buildClient(proxy string) (*http.Client, error) {
	tr := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout: 10 * time.Second,
		}).DialContext,
	}

	if proxy != "" {
		p, err := url.Parse(proxy)
		if err != nil {
			return nil, err
		}
		tr.Proxy = http.ProxyURL(p)
	}

	return &http.Client{
		Transport: tr,
		Timeout:   15 * time.Second,
	}, nil
}

func decodeBase64(s string) (string, error) {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "-", "+")
	s = strings.ReplaceAll(s, "_", "/")
	for len(s)%4 != 0 {
		s += "="
	}
	b, err := base64.StdEncoding.DecodeString(s)
	return string(b), err
}

func normalize(s string) []string {
	lines := strings.Split(s, "\n")
	seen := map[string]bool{}
	var out []string

	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" || seen[l] {
			continue
		}
		seen[l] = true
		out = append(out, l)
	}
	return out
}

func loadExisting(path string) []string {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return normalize(string(b))
}

func diff(old, new []string) (added, unchanged, removed int) {
	oldMap := map[string]bool{}
	for _, o := range old {
		oldMap[o] = true
	}

	newMap := map[string]bool{}
	for _, n := range new {
		newMap[n] = true
		if oldMap[n] {
			unchanged++
		} else {
			added++
		}
	}

	for _, o := range old {
		if !newMap[o] {
			removed++
		}
	}

	return
}

func printNetError(err error) {
	var netErr net.Error
	if errors.As(err, &netErr) {
		if netErr.Timeout() {
			fmt.Println("❌ Network timeout")
		} else {
			fmt.Println("❌ Network error")
		}
		return
	}

	if ue, ok := err.(*url.Error); ok {
		fmt.Println("❌ Connection error:", ue.Err)
		return
	}

	fmt.Println("❌ Request failed:", err)
}

func sanitizeFileName(link string) string {
	link = strings.TrimPrefix(link, "http://")
	link = strings.TrimPrefix(link, "https://")
	replacer := strings.NewReplacer(
		"/", "_",
		"\\", "_",
		":", "_",
		"*", "_",
		"?", "_",
		"\"", "_",
		"<", "_",
		">", "_",
		"|", "_",
		"&", "_",
		"=", "_",
	)
	return replacer.Replace(link)
}

func isURL(str string) bool {
	u, err := url.Parse(str)
	return err == nil && u.Scheme != "" && u.Host != ""
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
