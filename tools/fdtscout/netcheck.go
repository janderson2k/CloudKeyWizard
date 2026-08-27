package main

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Shared check primitives used by both the scheduled Monitoring engine (monitors.go) and the
// on-demand Pushbullet "ping <host>" command -- one implementation of each check type, not two
// copies that could drift.

// pingHost shells out to the system `ping` binary rather than hand-rolling raw ICMP -- consistent
// with this codebase's existing pattern (timedatectl, lsblk, ps, etc. are all real system tools,
// not reimplemented) and avoids needing CAP_NET_RAW/raw-socket handling for a single ICMP echo.
func pingHost(host string, timeoutSec int) (up bool, latencyMs float64, detail string) {
	if timeoutSec <= 0 {
		timeoutSec = 2
	}
	out, err := exec.Command("ping", "-c", "1", "-W", strconv.Itoa(timeoutSec), host).CombinedOutput()
	text := string(out)
	if err != nil {
		return false, 0, firstNonEmptyLine(text)
	}
	if m := pingTimeRe.FindStringSubmatch(text); len(m) == 2 {
		if v, perr := strconv.ParseFloat(m[1], 64); perr == nil {
			return true, v, ""
		}
	}
	return true, 0, ""
}

var pingTimeRe = regexp.MustCompile(`time=([0-9.]+)`)

func firstNonEmptyLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return "no response"
}

// tcpCheck reports whether a TCP connection to host:port succeeds within the timeout, and how long
// the connect took.
func tcpCheck(host string, port int, timeoutSec int) (up bool, latencyMs float64, detail string) {
	if timeoutSec <= 0 {
		timeoutSec = 3
	}
	start := time.Now()
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), time.Duration(timeoutSec)*time.Second)
	if err != nil {
		return false, 0, err.Error()
	}
	defer conn.Close()
	return true, float64(time.Since(start).Milliseconds()), ""
}

// httpCheck fetches url and reports status code + response time, optionally verifying the body
// contains expectedText (empty = no content check, status code alone is enough).
func httpCheck(url, expectedText string, timeoutSec int) (up bool, statusCode int, latencyMs float64, detail string) {
	if timeoutSec <= 0 {
		timeoutSec = 8
	}
	client := &http.Client{
		Timeout: time.Duration(timeoutSec) * time.Second,
		// InsecureSkipVerify deliberately allowed here -- this is a user-supplied URL for a health
		// check the user controls (e.g. their own self-signed FDT.Scout instance, a home NAS with a
		// self-signed cert), not a security-sensitive fetch of untrusted content. The response body
		// is only ever compared against an expected substring, never rendered or executed.
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
	}
	start := time.Now()
	resp, err := client.Get(url)
	if err != nil {
		return false, 0, 0, err.Error()
	}
	defer resp.Body.Close()
	latency := float64(time.Since(start).Milliseconds())

	if expectedText != "" {
		buf := make([]byte, 65536)
		n, _ := resp.Body.Read(buf)
		if !strings.Contains(string(buf[:n]), expectedText) {
			return false, resp.StatusCode, latency, "response did not contain expected text"
		}
	}
	return resp.StatusCode < 400, resp.StatusCode, latency, ""
}

// dnsCheck resolves host and reports what it found.
func dnsCheck(host string, timeoutSec int) (up bool, addresses []string, detail string) {
	if timeoutSec <= 0 {
		timeoutSec = 5
	}
	resolver := &net.Resolver{}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSec)*time.Second)
	defer cancel()
	addrs, err := resolver.LookupHost(ctx, host)
	if err != nil {
		return false, nil, err.Error()
	}
	return len(addrs) > 0, addrs, ""
}
