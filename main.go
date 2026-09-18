package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Config holds the proxy configuration
type Config struct {
	Port              int           `json:"port"`
	AllowedOrigins    []string      `json:"allowed_origins"`
	AllowedMethods    []string      `json:"allowed_methods"`
	AllowedHeaders    []string      `json:"allowed_headers"`
	ExposedHeaders    []string      `json:"exposed_headers"`
	MaxAge            int           `json:"max_age"`
	AllowCredentials  bool          `json:"allow_credentials"`
	Timeout           time.Duration `json:"timeout"`
	StripPathPrefix   string        `json:"strip_path_prefix"`
	LogRequests       bool          `json:"log_requests"`
	RateLimitEnabled  bool          `json:"rate_limit_enabled"`
	RateLimitRequests int           `json:"rate_limit_requests"`
	RateLimitWindow   time.Duration `json:"rate_limit_window"`
}

// DefaultConfig returns a default configuration
func DefaultConfig() *Config {
	return &Config{
		Port:              8080,
		AllowedOrigins:    []string{"*"},
		AllowedMethods:    []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS", "HEAD"},
		AllowedHeaders:    []string{"*"},
		ExposedHeaders:    []string{},
		MaxAge:            86400,
		AllowCredentials:  false,
		Timeout:           30 * time.Second,
		StripPathPrefix:   "",
		LogRequests:       true,
		RateLimitEnabled:  false,
		RateLimitRequests: 100,
		RateLimitWindow:   time.Minute,
	}
}

// RateLimiter tracks request counts per IP
type RateLimiter struct {
	requests map[string][]time.Time
	mu       sync.Mutex
	limit    int
	window   time.Duration
}

func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		requests: make(map[string][]time.Time),
		limit:    limit,
		window:   window,
	}
}

func (rl *RateLimiter) Allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-rl.window)

	// Filter old requests
	var valid []time.Time
	for _, t := range rl.requests[ip] {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}
	rl.requests[ip] = valid

	if len(valid) >= rl.limit {
		return false
	}

	rl.requests[ip] = append(rl.requests[ip], now)
	return true
}

var (
	configFile = flag.String("config", "", "Path to configuration file")
	port       = flag.Int("port", 8080, "Port to listen on")
)

func loadConfig() (*Config, error) {
	cfg := DefaultConfig()

	if *configFile != "" {
		data, err := os.ReadFile(*configFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}
		if err := json.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("failed to parse config file: %w", err)
		}
	}

	// Override with command line flags if provided
	if *port != 8080 {
		cfg.Port = *port
	}

	return cfg, nil
}

// CORSProxy handles CORS proxying
type CORSProxy struct {
	config      *Config
	rateLimiter *RateLimiter
	client      *http.Client
}

func NewCORSProxy(cfg *Config) *CORSProxy {
	proxy := &CORSProxy{
		config: cfg,
		client: &http.Client{
			Timeout: cfg.Timeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}

	if cfg.RateLimitEnabled {
		proxy.rateLimiter = NewRateLimiter(cfg.RateLimitRequests, cfg.RateLimitWindow)
	}

	return proxy
}

func (p *CORSProxy) getClientIP(r *http.Request) string {
	forwarded := r.Header.Get("X-Forwarded-For")
	if forwarded != "" {
		parts := strings.Split(forwarded, ",")
		return strings.TrimSpace(parts[0])
	}
	ip := r.RemoteAddr
	if colonIdx := strings.LastIndex(ip, ":"); colonIdx != -1 {
		ip = ip[:colonIdx]
	}
	return ip
}

func (p *CORSProxy) setCorsHeaders(w http.ResponseWriter, origin string) {
	if len(p.config.AllowedOrigins) == 1 && p.config.AllowedOrigins[0] == "*" {
		w.Header().Set("Access-Control-Allow-Origin", "*")
	} else {
		for _, o := range p.config.AllowedOrigins {
			if o == origin || o == "*" {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				break
			}
		}
	}

	w.Header().Set("Access-Control-Allow-Methods", strings.Join(p.config.AllowedMethods, ", "))
	w.Header().Set("Access-Control-Allow-Headers", strings.Join(p.config.AllowedHeaders, ", "))
	w.Header().Set("Access-Control-Expose-Headers", strings.Join(p.config.ExposedHeaders, ", "))
	w.Header().Set("Access-Control-Max-Age", strconv.Itoa(p.config.MaxAge))

	if p.config.AllowCredentials {
		w.Header().Set("Access-Control-Allow-Credentials", "true")
	}
}

func (p *CORSProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if p.config.LogRequests {
		log.Printf("[%s] %s %s", r.Method, r.URL.Path, r.RemoteAddr)
	}

	// Handle OPTIONS preflight
	if r.Method == http.MethodOptions {
		origin := r.Header.Get("Origin")
		if origin != "" {
			p.setCorsHeaders(w, origin)
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Rate limiting
	if p.rateLimiter != nil {
		ip := p.getClientIP(r)
		if !p.rateLimiter.Allow(ip) {
			http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
			return
		}
	}

	// Extract target URL from path or query parameter
	targetURLStr := r.URL.Query().Get("url")
	if targetURLStr == "" {
		targetURLStr = r.URL.Path
		if p.config.StripPathPrefix != "" {
			targetURLStr = strings.TrimPrefix(targetURLStr, p.config.StripPathPrefix)
		}
		// Remove leading slash
		targetURLStr = strings.TrimPrefix(targetURLStr, "/")
	}

	if targetURLStr == "" {
		http.Error(w, "Missing target URL. Use /<url> or /?url=<url>", http.StatusBadRequest)
		return
	}

	// Decode URL if it's encoded
	decoded, err := url.QueryUnescape(targetURLStr)
	if err == nil && strings.HasPrefix(decoded, "http") {
		targetURLStr = decoded
	}

	// Parse target URL
	targetURL, err := url.Parse(targetURLStr)
	if err != nil {
		// Try prepending https://
		targetURL, err = url.Parse("https://" + targetURLStr)
		if err != nil {
			http.Error(w, "Invalid target URL", http.StatusBadRequest)
			return
		}
	}

	// Validate URL scheme
	if targetURL.Scheme != "http" && targetURL.Scheme != "https" {
		http.Error(w, "Only HTTP and HTTPS schemes are allowed", http.StatusBadRequest)
		return
	}

	// Create proxied request
	proxiedReq, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL.String(), r.Body)
	if err != nil {
		http.Error(w, "Failed to create request", http.StatusInternalServerError)
		return
	}

	// Copy headers (exclude hop-by-hop headers)
	excludeHeaders := map[string]bool{
		"Host":              true,
		"Connection":        true,
		"Keep-Alive":        true,
		"Proxy-Authenticate": true,
		"Proxy-Authorization": true,
		"TE":                true,
		"Trailers":          true,
		"Transfer-Encoding": true,
		"Upgrade":           true,
	}

	for name, values := range r.Header {
		if !excludeHeaders[name] {
			for _, value := range values {
				proxiedReq.Header.Add(name, value)
			}
		}
	}

	// Set Host header
	proxiedReq.Host = targetURL.Host

	// Execute request
	resp, err := p.client.Do(proxiedReq)
	if err != nil {
		if ctxErr := r.Context().Err(); ctxErr != nil {
			http.Error(w, "Request timeout", http.StatusGatewayTimeout)
			return
		}
		http.Error(w, "Failed to fetch target", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Copy response headers
	excludeRespHeaders := map[string]bool{
		"Connection":      true,
		"Keep-Alive":      true,
		"Proxy-Authenticate": true,
		"Proxy-Authorization": true,
		"TE":              true,
		"Trailers":        true,
		"Transfer-Encoding": true,
		"Upgrade":         true,
	}

	for name, values := range resp.Header {
		if !excludeRespHeaders[name] {
			for _, value := range values {
				w.Header().Add(name, value)
			}
		}
	}

	// Set CORS headers
	origin := r.Header.Get("Origin")
	if origin != "" {
		p.setCorsHeaders(w, origin)
	}

	// Write status code
	w.WriteHeader(resp.StatusCode)

	// Copy response body
	io.Copy(w, resp.Body)
}

func main() {
	flag.Parse()

	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	proxy := NewCORSProxy(cfg)

	http.HandleFunc("/", proxy.ServeHTTP)

	addr := fmt.Sprintf(":%d", cfg.Port)
	log.Printf("AnyOrigin CORS Proxy starting on %s", addr)
	log.Printf("Configuration: %+v", cfg)

	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
