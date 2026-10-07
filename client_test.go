package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestPrivateHTTPRequestsBypassDefaultProxy(t *testing.T) {
	var proxyRequests atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		proxyRequests.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer proxy.Close()
	proxyURL, err := url.Parse(proxy.URL)
	if err != nil {
		t.Fatal(err)
	}
	originalTransport := http.DefaultTransport
	proxyTransport := originalTransport.(*http.Transport).Clone()
	proxyTransport.Proxy = func(*http.Request) (*url.URL, error) { return proxyURL, nil }
	http.DefaultTransport = proxyTransport
	t.Cleanup(func() {
		http.DefaultTransport = originalTransport
		proxyTransport.CloseIdleConnections()
	})
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != "test-aura-token" {
			t.Error("Aura did not receive its API token")
		}
		writeEnvelope(w, map[string]interface{}{"sections": []auraLibrary{{Title: "Movies", Type: "movie"}}})
	}))
	defer api.Close()
	client, err := (connectionConfig{Address: api.URL, APIToken: "test-aura-token"}).client()
	if err != nil {
		t.Fatal(err)
	}
	libraries, err := client.libraries(context.Background())
	if err != nil || len(libraries) != 1 || proxyRequests.Load() != 0 {
		t.Fatalf("private request used a proxy: libraries=%d proxy requests=%d error=%v", len(libraries), proxyRequests.Load(), err)
	}
}

func TestPrivateHTTPClientsReuseConnections(t *testing.T) {
	var connections atomic.Int32
	api := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeEnvelope(w, map[string]interface{}{"sections": []auraLibrary{{Title: "Movies", Type: "movie"}}})
	}))
	api.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connections.Add(1)
		}
	}
	api.Start()
	defer api.Close()
	for range 2 {
		client, err := (connectionConfig{Address: api.URL, APIToken: "test-aura-token"}).client()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := client.libraries(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if connections.Load() != 1 {
		t.Fatalf("separate clients opened %d connections; want one reused connection", connections.Load())
	}
}

func TestAuraFailures(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		httpCode   int
		want       codes.Code
	}{
		{"unauthorized", `{}`, 401, codes.Unauthenticated},
		{"forbidden", `{}`, 403, codes.Unauthenticated},
		{"rate limit", `{}`, 429, codes.ResourceExhausted},
		{"outage", `<html>unavailable</html>`, 503, codes.Unavailable},
		{"invalid JSON", `not-json`, 200, codes.DataLoss},
		{"missing data", `{"status":"success"}`, 200, codes.DataLoss},
		{"wrong fields", `{"status":"success","data":{"sections":"wrong"}}`, 200, codes.DataLoss},
		{"empty libraries", `{"status":"success","data":{"sections":[]}}`, 200, codes.FailedPrecondition},
		{"unpopulated cache", `{"status":"error","error":{"message":"Library Cache Empty"}}`, 500, codes.FailedPrecondition},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.httpCode)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer api.Close()
			client, err := (connectionConfig{Address: api.URL, APIToken: "test-aura-token"}).client()
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.libraries(context.Background())
			if status.Code(err) != tc.want {
				t.Fatalf("want %s, got %v", tc.want, err)
			}
		})
	}
}

func TestAuraEmptySetsAndRedirect(t *testing.T) {
	forwarded := false
	other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { forwarded = true }))
	defer other.Close()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/mediux/sets/item" {
			w.WriteHeader(500)
			_, _ = w.Write([]byte(`{"status":"error","error":{"message":"No Sets Found"}}`))
			return
		}
		http.Redirect(w, r, other.URL, http.StatusFound)
	}))
	defer api.Close()
	client, _ := (connectionConfig{Address: api.URL, APIToken: "test-aura-token"}).client()
	var data map[string]interface{}
	if err := client.get(context.Background(), "/api/mediux/sets/item", nil, &data); !errors.Is(err, errNoArtwork) {
		t.Fatal("Aura's documented empty sets response should be empty", err)
	}
	if _, err := client.libraries(context.Background()); err == nil || forwarded {
		t.Fatal("redirect should fail without forwarding credentials", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.libraries(ctx); status.Code(err) != codes.Canceled {
		t.Fatal("cancellation was not propagated", err)
	}
}
