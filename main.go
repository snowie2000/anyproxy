package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
)

const maxRedirects = 20

var tr = &http.Transport{Proxy: http.ProxyFromEnvironment}

func copyHeaders(src http.Header, dst http.Header) {
	for k, vv := range src {
		lk := strings.ToLower(k)
		// Host header is managed by the Transport
		if lk == "host" || lk == "referer" || strings.HasPrefix(lk, "x-forwarded-") {
			continue
		}
		dst.Del(k)
		for _, v := range vv {
			dst.Set(k, v)
		}
	}
}

func proxyHandler(w http.ResponseWriter, r *http.Request) {
	targetURL := r.URL.Query().Get("url")
	userAgent := r.URL.Query().Get("User-Agent")
	if targetURL == "" {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	if userAgent != "" {
		r.Header.Set("User-Agent", userAgent)
	}

	var (
		tr         = tr
		redirects  = 0
		currentURL = targetURL
		visited    = map[string]struct{}{}
	)

	for {
		// Prevent redirect loops
		if _, exists := visited[currentURL]; exists || redirects >= maxRedirects {
			http.Error(w, fmt.Sprintf("Redirect loop detected after %d redirects", redirects), http.StatusInternalServerError)
			return
		}
		visited[currentURL] = struct{}{}
		redirects++

		req, err := http.NewRequest(http.MethodGet, currentURL, nil)
		if err != nil {
			http.Error(w, "Error creating request", http.StatusInternalServerError)
			return
		}
		copyHeaders(r.Header, req.Header)

		resp, err := tr.RoundTrip(req)
		if err != nil {
			http.Error(w, fmt.Sprintf("Upstream error: %s", err), http.StatusBadGateway)
			return
		}

		// Manual redirect handling
		if resp.StatusCode >= 300 && resp.StatusCode <= 399 {
			resp.Body.Close()
			loc := resp.Header.Get("Location")
			if loc == "" {
				http.Error(w, "Redirect response missing Location header", http.StatusBadGateway)
				return
			}
			// Resolve relative redirects
			u, err := url.Parse(loc)
			if err != nil {
				http.Error(w, "Invalid redirect URL received", http.StatusBadGateway)
				return
			}
			if !u.IsAbs() {
				base, _ := url.Parse(currentURL)
				loc = base.ResolveReference(u).String()
			}
			currentURL = loc
			continue
		}

		// Write headers and status
		copyHeaders(resp.Header, w.Header())
		w.WriteHeader(resp.StatusCode)

		// Write body as streaming (no buffering)
		_, err = io.Copy(w, resp.Body)
		resp.Body.Close()
		if err != nil {
			log.Printf("io.Copy streaming error: %v", err)
		}
		return
	}
}

func main() {
	port := "9090"
	http.HandleFunc("/", proxyHandler)
	fmt.Printf("anyproxy streaming on http://localhost:%s\n", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
