// Copyright (c) 2026 arumes31
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package auth

import (
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
)

// defaultTrustedProxies covers loopback and private networks, where a
// reverse proxy or Docker network gateway normally connects from.
const defaultTrustedProxies = "127.0.0.0/8,::1/128,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,fc00::/7"

// parseTrustedProxies parses a comma-separated list of CIDRs or single IPs.
func parseTrustedProxies(value string) []*net.IPNet {
	if strings.TrimSpace(value) == "" {
		value = defaultTrustedProxies
	}
	var networks []*net.IPNet
	for _, entry := range strings.Split(value, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if ip := net.ParseIP(entry); ip != nil {
			bits := 8 * len(ip)
			if ip4 := ip.To4(); ip4 != nil {
				ip, bits = ip4, 32
			}
			networks = append(networks, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
			continue
		}
		_, network, err := net.ParseCIDR(entry)
		if err != nil {
			slog.Warn("auth: ignoring invalid TRUSTED_PROXIES entry", "entry", entry)
			continue
		}
		networks = append(networks, network)
	}
	return networks
}

func isTrustedProxy(ip net.IP, trusted []*net.IPNet) bool {
	if ip == nil {
		return false
	}
	for _, network := range trusted {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

// parseHeaderIP returns the canonical form of a header IP, or "" if invalid.
func parseHeaderIP(value string) string {
	ip := net.ParseIP(strings.TrimSpace(value))
	if ip == nil {
		return ""
	}
	return ip.String()
}

// forwardedClientIP walks X-Forwarded-For from the right and returns the first
// hop that is not a trusted proxy. Entries left of it are client-controlled.
func forwardedClientIP(xff string, trusted []*net.IPNet) string {
	hops := strings.Split(xff, ",")
	for i := len(hops) - 1; i >= 0; i-- {
		ip := net.ParseIP(strings.TrimSpace(hops[i]))
		if ip == nil {
			return ""
		}
		if !isTrustedProxy(ip, trusted) || i == 0 {
			return ip.String()
		}
	}
	return ""
}

// ProxyMiddleware handles reverse proxy and Cloudflare headers to extract the real client IP.
// Controllable via TRUST_PROXY, TRUSTED_PROXIES and TRUST_CLOUDFLARE environment variables.
func ProxyMiddleware(next http.Handler) http.Handler {
	// Security: Fail-secure default. Only trust proxy headers if explicitly enabled.
	trustProxy := os.Getenv("TRUST_PROXY") == "true"
	trustCF := os.Getenv("TRUST_CLOUDFLARE") == "true"
	trusted := parseTrustedProxies(os.Getenv("TRUSTED_PROXIES"))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var realIP string

		if trustCF {
			// Cloudflare specific header
			realIP = parseHeaderIP(r.Header.Get("CF-Connecting-IP"))
		}

		// Forwarding headers are only honoured from a trusted proxy peer;
		// otherwise any client could pick its own rate-limit bucket.
		if realIP == "" && trustProxy && isTrustedProxy(net.ParseIP(clientIP(r)), trusted) {
			if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
				realIP = forwardedClientIP(xff, trusted)
			} else {
				realIP = parseHeaderIP(r.Header.Get("X-Real-IP"))
			}
		}

		// If we found a real IP, we update r.RemoteAddr
		// Note: RemoteAddr usually contains the port, but we'll store the IP here for simplicity
		// as many internal logs/checks only look at this field.
		if realIP != "" {
			r.RemoteAddr = realIP
		}

		next.ServeHTTP(w, r)
	})
}
