package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	requestTimeout   = 20 * time.Second
	maxResponseBytes = 16 << 20
)

var errNoArtwork = errors.New("Aura has no artwork sets for this title")

var directAuraTransport = func() *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// Keep private Aura credentials off proxies while sharing connection pools.
	transport.Proxy = nil
	return transport
}()

type connectionConfig struct {
	Address  string `json:"address"`
	APIToken string `json:"api_token"`
}

type auraClient struct {
	baseURL  *url.URL
	apiToken string
	http     *http.Client
}

func (c connectionConfig) client() (*auraClient, error) {
	address, apiToken := strings.TrimSpace(c.Address), strings.TrimSpace(c.APIToken)
	if address == "" || apiToken == "" {
		return nil, status.Error(codes.FailedPrecondition, "Configure the Aura address and API token in Silo's plugin settings.")
	}
	if !strings.Contains(address, "://") {
		address = "http://" + address
	}
	u, err := url.Parse(address)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, status.Error(codes.FailedPrecondition, "Aura address must be an HTTP or HTTPS URL without credentials, query parameters or a fragment.")
	}
	if u.Scheme != "https" {
		ip := net.ParseIP(u.Hostname())
		if u.Scheme != "http" || (u.Hostname() != "localhost" && (ip == nil || (!ip.IsPrivate() && !ip.IsLoopback()))) {
			return nil, status.Error(codes.FailedPrecondition, "Use HTTPS for Aura, or HTTP with a private or loopback IP address.")
		}
	}
	if strings.ContainsAny(apiToken, "\r\n") {
		return nil, status.Error(codes.FailedPrecondition, "Aura API token cannot contain a newline.")
	}
	transport := http.DefaultTransport
	if u.Scheme == "http" {
		transport = directAuraTransport
	}
	return &auraClient{
		baseURL: u, apiToken: apiToken,
		http: &http.Client{Transport: transport, Timeout: requestTimeout, CheckRedirect: func(*http.Request, []*http.Request) error {
			// A redirect must not forward the token to another origin.
			return http.ErrUseLastResponse
		}},
	}, nil
}

type auraEnvelope struct {
	Status string          `json:"status"`
	Data   json.RawMessage `json:"data"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (c *auraClient) get(ctx context.Context, path string, query url.Values, target interface{}) error {
	u := c.baseURL.JoinPath(path)
	u.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return status.Error(codes.FailedPrecondition, "Aura address could not form a request.")
	}
	req.Header.Set("X-Api-Key", c.apiToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "silo-plugin-metadata-aura/"+version)
	response, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return status.FromContextError(ctx.Err()).Err()
		}
		return status.Error(codes.Unavailable, "Aura could not be reached; check its address and availability.")
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return status.Error(codes.Unauthenticated, "Aura rejected the API token.")
	}
	if response.StatusCode == http.StatusTooManyRequests {
		return status.Error(codes.ResourceExhausted, "Aura is rate limiting requests; try again later.")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil || len(body) > maxResponseBytes {
		return status.Error(codes.DataLoss, "Aura response could not be read or exceeded the size limit.")
	}
	var envelope auraEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return status.Errorf(codes.Unavailable, "Aura reported a failed request (HTTP %d).", response.StatusCode)
		}
		return status.Errorf(codes.DataLoss, "Aura returned an invalid JSON response (HTTP %d).", response.StatusCode)
	}
	if envelope.Error != nil {
		switch envelope.Error.Message {
		case "No Sets Found":
			if path == "/api/mediux/sets/item" {
				return errNoArtwork
			}
		case "Library Cache Empty", "No library sections found":
			return status.Error(codes.FailedPrecondition, "Aura has no usable library cache; refresh its libraries in Aura.")
		}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 || envelope.Status == "error" || envelope.Error != nil {
		return status.Errorf(codes.Unavailable, "Aura reported a failed request (HTTP %d).", response.StatusCode)
	}
	if (envelope.Status != "success" && envelope.Status != "warn") || len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return status.Error(codes.DataLoss, "Aura returned no valid response data.")
	}
	if err := json.Unmarshal(envelope.Data, target); err != nil {
		return status.Error(codes.DataLoss, "Aura response fields do not match the artwork API.")
	}
	return nil
}

type auraLibrary struct {
	Title string `json:"title"`
	Type  string `json:"type"`
}

func (c *auraClient) libraries(ctx context.Context) ([]auraLibrary, error) {
	var data struct {
		Sections []auraLibrary `json:"sections"`
	}
	if err := c.get(ctx, "/api/mediaserver/libraries", nil, &data); err != nil {
		return nil, err
	}
	if len(data.Sections) == 0 {
		return nil, status.Error(codes.FailedPrecondition, "Aura has no configured libraries.")
	}
	return data.Sections, nil
}

func (c *auraClient) libraryTitle(ctx context.Context, itemType string) (string, error) {
	libraries, err := c.libraries(ctx)
	if err != nil {
		return "", err
	}
	for _, library := range libraries {
		// Aura selects sets by TMDB ID; the library only enriches included items.
		if library.Type == itemType && strings.TrimSpace(library.Title) != "" {
			return library.Title, nil
		}
	}
	return "", status.Error(codes.FailedPrecondition, fmt.Sprintf("Aura has no configured %s library.", itemType))
}
