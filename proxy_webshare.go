package goserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

type WebshareProvider struct {
	HTTPClient *http.Client
	Token      string
}

type webshareListResponse struct {
	Next    *string `json:"next"`
	Results []struct {
		Username     string `json:"username"`
		Password     string `json:"password"`
		ProxyAddress string `json:"proxy_address"`
		Port         int    `json:"port"`
		Valid        bool   `json:"valid"`
	} `json:"results"`
}

func (w *WebshareProvider) FetchProxies(ctx context.Context) ([]*url.URL, error) {
	if w.Token == "" {
		return nil, fmt.Errorf("webshare token is empty")
	}

	client := w.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}

	currentURL := "https://proxy.webshare.io/api/v2/proxy/list/?mode=direct&page_size=100"
	var parsedProxies []*url.URL

	for currentURL != "" {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, currentURL, nil)
		if err != nil {
			return nil, err
		}

		req.Header.Set("Authorization", "Token "+w.Token)

		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("error fetching webshare api: %w", err)
		}
		defer func() {
			_ = resp.Body.Close()
		}()

		var data webshareListResponse
		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			return nil, fmt.Errorf("error parsing json from webshare: %w", err)
		}

		for _, p := range data.Results {
			if !p.Valid {
				continue
			}

			proxyURL := &url.URL{
				Scheme: "http",
				User:   url.UserPassword(p.Username, p.Password),
				Host:   fmt.Sprintf("%s:%d", p.ProxyAddress, p.Port),
			}
			parsedProxies = append(parsedProxies, proxyURL)
		}

		if data.Next != nil {
			currentURL = *data.Next
		} else {
			currentURL = ""
		}
	}

	return parsedProxies, nil
}
