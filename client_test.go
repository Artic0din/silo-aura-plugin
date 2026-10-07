package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

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
