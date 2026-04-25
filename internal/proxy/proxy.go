package proxy

import (
	"net/http"
	"strings"

	"github.com/seelly/llm-auto-fallback/internal/forwarder"
)

// Handler is the HTTP handler for the fallback gateway.
type Handler struct {
	fwd *forwarder.Forwarder
}

func New(fwd *forwarder.Forwarder) *Handler {
	return &Handler{fwd: fwd}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	switch {
	case (path == "/v1/models" || path == "/models") && r.Method == http.MethodGet:
		h.fwd.ProxyModels(w, r)

	case (path == "/v1/chat/completions" || path == "/chat/completions") && r.Method == http.MethodPost:
		h.fwd.ProxyRequest(w, r, "chat/completions")

	case (path == "/v1/completions" || path == "/completions") && r.Method == http.MethodPost:
		h.fwd.ProxyRequest(w, r, "completions")

	case (path == "/v1/responses" || path == "/responses") && r.Method == http.MethodPost:
		h.fwd.ProxyRequest(w, r, "responses")

	case (strings.HasPrefix(path, "/v1/responses/") || strings.HasPrefix(path, "/responses/")) && r.Method == http.MethodGet:
		// /v1/responses/{response_id} or /responses/{response_id}
		responseID := strings.TrimPrefix(path, "/v1/responses/")
		responseID = strings.TrimPrefix(responseID, "/responses/")
		h.fwd.ProxyRequest(w, r, "responses/"+responseID)

	default:
		// Pass through any other /v1/ paths
		if strings.HasPrefix(path, "/v1/") {
			trimmedPath := strings.TrimPrefix(path, "/v1/")
			h.fwd.ProxyRequest(w, r, trimmedPath)
		} else if strings.HasPrefix(path, "/") && path != "/" {
			// Also support paths without /v1/ prefix
			trimmedPath := strings.TrimPrefix(path, "/")
			h.fwd.ProxyRequest(w, r, trimmedPath)
		} else {
			http.NotFound(w, r)
		}
	}
}
