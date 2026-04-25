package forwarder

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/seelly/llm-auto-fallback/internal/config"
	"github.com/seelly/llm-auto-fallback/internal/fallback"
)

// Forwarder proxies requests to upstream providers with fallback support.
type Forwarder struct {
	cfg    *config.Config
	engine *fallback.Engine
	client *http.Client
}

func New(cfg *config.Config, engine *fallback.Engine) *Forwarder {
	return &Forwarder{
		cfg:    cfg,
		engine: engine,
		client: &http.Client{
			Timeout: 0, // no timeout for streaming requests
			Transport: &http.Transport{
				MaxIdleConns:       100,
				IdleConnTimeout:    90 * time.Second,
				DisableCompression: false,
			},
		},
	}
}

// ProxyRequest forwards a request to the appropriate upstream provider.
// It handles model resolution, fallback, and SSE streaming transparently.
func (f *Forwarder) ProxyRequest(w http.ResponseWriter, r *http.Request, path string) {
	// Read the request body
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Extract model from request
	preferredModel := extractModel(bodyBytes)
	if preferredModel == "" {
		http.Error(w, "model field not found in request", http.StatusBadRequest)
		return
	}

	// Resolve model via fallback engine
	resolvedModel, providerName := f.engine.Resolve(preferredModel)
	if resolvedModel == "" {
		http.Error(w, "no available model found", http.StatusServiceUnavailable)
		return
	}

	prov := f.cfg.ProviderByName(providerName)
	if prov == nil {
		http.Error(w, "provider not found: "+providerName, http.StatusInternalServerError)
		return
	}

	// Rewrite model in request body if fallback occurred
	if resolvedModel != preferredModel {
		bodyBytes = rewriteModel(bodyBytes, resolvedModel)
		log.Printf("[forwarder] fallback: %s -> %s @ %s", preferredModel, resolvedModel, providerName)
	}

	// Build upstream URL
	upstreamURL := strings.TrimRight(prov.BaseURL, "/") + "/" + strings.TrimLeft(path, "/")

	// Check if this is a streaming request
	isStream := isStreamingRequest(bodyBytes)

	// Create upstream request
	upReq, err := http.NewRequestWithContext(r.Context(), r.Method, upstreamURL, bytes.NewReader(bodyBytes))
	if err != nil {
		http.Error(w, "failed to create upstream request", http.StatusInternalServerError)
		return
	}

	// Copy headers (except hop-by-hop)
	copyHeaders(upReq.Header, r.Header)
	upReq.Header.Set("Authorization", "Bearer "+prov.APIKey)
	upReq.Header.Set("Host", "")

	// Execute request
	if isStream {
		f.proxyStream(w, upReq)
	} else {
		f.proxyRegular(w, upReq)
	}
}

func (f *Forwarder) proxyRegular(w http.ResponseWriter, upReq *http.Request) {
	resp, err := f.client.Do(upReq)
	if err != nil {
		http.Error(w, "upstream request failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	copyHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func (f *Forwarder) proxyStream(w http.ResponseWriter, upReq *http.Request) {
	resp, err := f.client.Do(upReq)
	if err != nil {
		http.Error(w, "upstream request failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Non-200 on stream request: proxy the error response normally
		copyHeaders(w.Header(), resp.Header)
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	// Read SSE events from upstream and write to client
	buf := make([]byte, 4096)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := w.Write(buf[:n]); writeErr != nil {
				return
			}
			flusher.Flush()
		}
		if readErr != nil {
			if readErr != io.EOF {
				log.Printf("[forwarder] stream read error: %v", readErr)
			}
			break
		}
	}
}

// ProxyModels aggregates /v1/models from all providers.
// Only returns currently available models based on probe results.
func (f *Forwarder) ProxyModels(w http.ResponseWriter, r *http.Request) {
	availableModels := f.engine.Prober().GetAvailableModels()

	type modelData struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		OwnedBy string `json:"owned_by"`
	}

	var data []modelData
	for _, status := range availableModels {
		data = append(data, modelData{
			ID:      status.Model,
			Object:  "model",
			Created: 0,
			OwnedBy: status.Provider,
		})
	}

	resp := map[string]interface{}{
		"object": "list",
		"data":   data,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// Prober exposes the prober for the proxy layer.
func (f *Forwarder) Engine() *fallback.Engine {
	return f.engine
}

func extractModel(body []byte) string {
	var req struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return ""
	}
	return req.Model
}

func rewriteModel(body []byte, newModel string) []byte {
	var req map[string]interface{}
	if err := json.Unmarshal(body, &req); err != nil {
		return body
	}
	req["model"] = newModel
	result, err := json.Marshal(req)
	if err != nil {
		return body
	}
	return result
}

func isStreamingRequest(body []byte) bool {
	var req struct {
		Stream bool `json:"stream"`
	}
	json.Unmarshal(body, &req)
	return req.Stream
}

var hopByHopHeaders = map[string]bool{
	"Connection":          true,
	"Keep-Alive":          true,
	"Proxy-Authenticate":  true,
	"Proxy-Authorization": true,
	"Te":                  true,
	"Trailer":             true,
	"Transfer-Encoding":   true,
	"Upgrade":             true,
}

func copyHeaders(dst, src http.Header) {
	for key, values := range src {
		if hopByHopHeaders[key] {
			continue
		}
		for _, v := range values {
			dst.Add(key, v)
		}
	}
}
