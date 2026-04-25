package prober

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/seelly/llm-auto-fallback/internal/config"
)

// ModelStatus represents the health status of a model.
type ModelStatus struct {
	Model    string
	Provider string
	Avail    bool
	Latency  time.Duration
	ProbedAt time.Time
}

// Prober manages model discovery and health probing across providers.
type Prober struct {
	cfg    *config.Config
	client *http.Client
	mu     sync.RWMutex
	status map[string]*ModelStatus // model -> status
	models map[string]string       // model -> provider name
	stopCh chan struct{}
}

func New(cfg *config.Config) *Prober {
	return &Prober{
		cfg:    cfg,
		client: &http.Client{Timeout: cfg.Probe.Timeout},
		status: make(map[string]*ModelStatus),
		models: make(map[string]string),
		stopCh: make(chan struct{}),
	}
}

// Start begins the periodic probing loop.
func (p *Prober) Start(ctx context.Context) {
	p.probeAll(ctx)

	ticker := time.NewTicker(p.cfg.Probe.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			p.probeAll(ctx)
		case <-p.stopCh:
			return
		case <-ctx.Done():
			return
		}
	}
}

// Stop halts the probing loop.
func (p *Prober) Stop() {
	close(p.stopCh)
}

// IsAvailable returns whether a model is currently available.
func (p *Prober) IsAvailable(model string) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	s, ok := p.status[model]
	return ok && s.Avail
}

// GetStatus returns the status for a model.
func (p *Prober) GetStatus(model string) *ModelStatus {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.status[model]
}

// GetAllModels returns all known models and their providers.
func (p *Prober) GetAllModels() map[string]string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	result := make(map[string]string, len(p.models))
	for k, v := range p.models {
		result[k] = v
	}
	return result
}

// GetAvailableModels returns all currently available models.
func (p *Prober) GetAvailableModels() []ModelStatus {
	p.mu.RLock()
	defer p.mu.RUnlock()
	var result []ModelStatus
	for _, s := range p.status {
		if s.Avail {
			result = append(result, *s)
		}
	}
	return result
}

// ProviderFor returns the provider name for a given model.
func (p *Prober) ProviderFor(model string) (string, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	prov, ok := p.models[model]
	return prov, ok
}

func (p *Prober) probeAll(ctx context.Context) {
	var wg sync.WaitGroup
	for i := range p.cfg.Providers {
		wg.Add(1)
		go func(prov *config.ProviderConfig) {
			defer wg.Done()
			p.probeProvider(ctx, prov)
		}(&p.cfg.Providers[i])
	}
	wg.Wait()
}

func (p *Prober) probeProvider(ctx context.Context, prov *config.ProviderConfig) {
	// Step 1: discover models via /v1/models
	models, err := p.discoverModels(ctx, prov)
	if err != nil {
		log.Printf("[prober] failed to discover models from %s: %v", prov.Name, err)
		return
	}
	log.Printf("discovered %+v models from %s \n", models, prov.Name)

	// Step 2: probe each model
	var wg sync.WaitGroup
	for _, model := range models {
		wg.Add(1)
		go func(m string) {
			defer wg.Done()
			p.probeModel(ctx, prov, m)
		}(model)
	}
	wg.Wait()
}

type modelsResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

func (p *Prober) discoverModels(ctx context.Context, prov *config.ProviderConfig) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, prov.BaseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	//fmt.Printf("prov.APIKey: %v\n", prov.APIKey)
	req.Header.Set("Authorization", "Bearer "+prov.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	var mr modelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&mr); err != nil {
		return nil, err
	}

	var models []string
	for _, d := range mr.Data {
		models = append(models, d.ID)
	}

	// Update model -> provider mapping
	p.mu.Lock()
	for _, m := range models {
		if existing, ok := p.models[m]; ok && existing != prov.Name {
			log.Printf("[prober] model %q claimed by %s, already owned by %s (keeping first)", m, prov.Name, existing)
			continue
		}
		p.models[m] = prov.Name
	}
	p.mu.Unlock()

	return models, nil
}

type probeRequest struct {
	Model     string    `json:"model"`
	Messages  []message `json:"messages"`
	MaxTokens int       `json:"max_tokens"`
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func (p *Prober) probeModel(ctx context.Context, prov *config.ProviderConfig, model string) {
	start := time.Now()

	body := probeRequest{
		Model: model,
		Messages: []message{
			{Role: "user", Content: "hi"},
		},
		MaxTokens: 1,
	}

	bodyBytes, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, prov.BaseURL+"/chat/completions", bytes.NewReader(bodyBytes))
	if err != nil {
		p.updateStatus(model, prov.Name, false, 0)
		return
	}
	req.Header.Set("Authorization", "Bearer "+prov.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	latency := time.Since(start)

	if err != nil {
		p.updateStatus(model, prov.Name, false, latency)
		return
	}
	defer resp.Body.Close()

	avail := resp.StatusCode == http.StatusOK
	p.updateStatus(model, prov.Name, avail, latency)
}

func (p *Prober) updateStatus(model, provider string, avail bool, latency time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.status[model] = &ModelStatus{
		Model:    model,
		Provider: provider,
		Avail:    avail,
		Latency:  latency,
		ProbedAt: time.Now(),
	}
}
