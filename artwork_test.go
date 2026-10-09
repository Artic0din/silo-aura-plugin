package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	pluginv1 "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginproto/silo/plugin/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

const testModified = "2026-10-01T12:30:00+10:00"

func TestArtworkPicker(t *testing.T) {
	zero, one, two := int32(0), int32(1), int32(2)
	assets := []auraImage{
		{ID: "poster", Type: "poster", Modified: testModified, ItemTMDBID: "123", Language: "en"},
		{ID: "poster", Type: "poster", Modified: testModified, ItemTMDBID: "123"},
		{ID: "backdrop", Type: "backdrop", Modified: testModified, ItemTMDBID: "123", Language: "English"},
		{ID: "other-movie", Type: "poster", Modified: testModified, ItemTMDBID: "999"},
		{ID: "season-one", Type: "season_poster", Modified: testModified, ItemTMDBID: "123", SeasonNumber: &one},
		{ID: "specials", Type: "season_poster", Modified: testModified, ItemTMDBID: "123", SeasonNumber: &zero},
		{ID: "title-card", Type: "poster", Modified: testModified, ItemTMDBID: "123", SeasonNumber: &one, EpisodeNumber: &two},
	}
	for _, tc := range []struct {
		name, itemType string
		season         *int32
		want           []string
	}{
		{"movie", "movie", nil, []string{"poster", "backdrop"}},
		{"series", "series", nil, []string{"poster", "backdrop"}},
		{"season", "series", &one, []string{"season-one"}},
		{"specials", "series", &zero, []string{"specials"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Header.Get("X-Api-Key") != "test-aura-token" {
					t.Error("API token was not sent in its header")
				}
				if r.URL.Path == "/api/mediaserver/libraries" {
					writeEnvelope(w, map[string]interface{}{"sections": []auraLibrary{{Title: "Movies & TV", Type: "movie"}, {Title: "TV Shows", Type: "show"}}})
					return
				}
				if r.URL.Path != "/api/mediux/sets/item" || r.URL.Query().Get("tmdb_id") != "123" {
					t.Error("wrong artwork lookup")
				}
				library, itemType := "TV Shows", "show"
				if tc.itemType == "movie" {
					library, itemType = "Movies & TV", "movie"
				}
				if r.URL.Query().Get("item_library_title") != library || r.URL.Query().Get("item_type") != itemType {
					t.Error("lookup did not use the discovered library and Aura item type")
				}
				writeEnvelope(w, map[string]interface{}{"sets": []interface{}{map[string]interface{}{"user_created": "poster-maker", "images": assets}}})
			}))
			defer api.Close()
			server := &artworkServer{configuration: connectionConfig{Address: api.URL, APIToken: "test-aura-token"}}
			ids, _ := structpb.NewStruct(map[string]interface{}{"tmdb": "123"})
			response, err := server.GetImages(context.Background(), &pluginv1.GetImagesRequest{ItemType: tc.itemType, ProviderIds: ids, SeasonNumber: tc.season})
			if err != nil {
				t.Fatal(err)
			}
			if calls != 2 || len(response.Images) != len(tc.want) {
				t.Fatalf("calls=%d images=%d; want 2 calls and %d images", calls, len(response.Images), len(tc.want))
			}
			for i, assetID := range tc.want {
				image := response.Images[i]
				if image.GetMetadata().GetFields()["creator"].GetStringValue() != "poster-maker" {
					t.Fatal("artwork is missing its set creator")
				}
				if !strings.HasPrefix(image.Url, artworkScheme+"image/"+assetID+"?") || image.SeasonNumber != tc.season {
					t.Fatalf("unexpected image %v", image)
				}
				if want, ok := map[string]string{"poster": "en", "backdrop": ""}[assetID]; ok && image.Language != want {
					t.Fatalf("image %s language = %q; want %q", assetID, image.Language, want)
				}
				resolved, err := server.resolveImage(image.Url, "original")
				if err != nil {
					t.Fatal(err)
				}
				parsed, _ := url.Parse(resolved)
				if parsed.Query().Get("asset_id") != assetID || parsed.Query().Get("modified_date") != testModified || parsed.Query().Get("quality") != "original" {
					t.Fatalf("wrong original URL %q", resolved)
				}
				if strings.Contains(resolved, "test-aura-token") {
					t.Fatal("credential leaked into image URL")
				}
			}
		})
	}
}

func TestImageResolution(t *testing.T) {
	server := &artworkServer{configuration: connectionConfig{Address: "10.0.0.10:8888", APIToken: "test-aura-token"}}
	path := "image/asset-id?" + url.Values{"modified_date": {testModified}}.Encode()
	for variant, quality := range map[string]string{"card": "thumb", "featured": "optimized", "large": "optimized", "full": "original", "original": "original", "future-variant": "original"} {
		for _, prefix := range []string{"", artworkScheme} {
			resolved, err := server.resolveImage(prefix+path, variant)
			if err != nil {
				t.Fatal(err)
			}
			u, _ := url.Parse(resolved)
			if u.Query().Get("quality") != quality {
				t.Fatalf("variant %q: want %s, got %s", variant, quality, u.Query().Get("quality"))
			}
		}
	}
	for _, path := range []string{"https://example.com/image/x", "image/../x", "image/x", "image/x?modified_date=bad", "image/x?modified_date=" + url.QueryEscape(testModified) + "&quality=thumb", "image/x?modified_date=" + url.QueryEscape(testModified) + "#fragment"} {
		if _, err := server.resolveImage(path, "card"); status.Code(err) != codes.InvalidArgument {
			t.Fatalf("invalid path accepted: %q (%v)", path, err)
		}
	}
}

func TestArtworkResponseBoundary(t *testing.T) {
	for _, tc := range []struct {
		body string
		want codes.Code
	}{
		{`{"status":"success","data":{}}`, codes.DataLoss},
		{`{"status":"success","data":{"sets":null}}`, codes.DataLoss},
		{`{"status":"success","data":{"sets":[]}}`, codes.OK},
		{`{"status":"error","error":{"message":"No Sets Found"}}`, codes.OK},
		{`{"status":"success","data":{"sets":[{"images":[{"id":"../asset","type":"poster","item_tmdb_id":"123","modified":"2026-10-01T00:00:00Z"}]}]}}`, codes.DataLoss},
	} {
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/mediaserver/libraries" {
				writeEnvelope(w, map[string]interface{}{"sections": []auraLibrary{{Title: "Movies", Type: "movie"}}})
				return
			}
			_, _ = w.Write([]byte(tc.body))
		}))
		server := &artworkServer{configuration: connectionConfig{Address: api.URL, APIToken: "test-aura-token"}}
		ids, _ := structpb.NewStruct(map[string]interface{}{"tmdb": "123"})
		_, err := server.GetImages(context.Background(), &pluginv1.GetImagesRequest{ItemType: "movie", ProviderIds: ids})
		api.Close()
		if status.Code(err) != tc.want {
			t.Fatalf("response %s: want %s, got %v", tc.body, tc.want, err)
		}
	}
}

func TestConfigurationAndMissingIdentity(t *testing.T) {
	for _, address := range []string{"10.0.0.10:8888", "http://127.0.0.1:8888", "https://aura.example.com"} {
		if _, err := (connectionConfig{Address: address, APIToken: "test-aura-token"}).client(); err != nil {
			t.Fatal(address, err)
		}
	}
	for _, address := range []string{"", "http://public.example.com", "ftp://10.0.0.10", "https://user:password@example.com", "https://example.com?secret=x", "https://example.com#fragment"} {
		if _, err := (connectionConfig{Address: address, APIToken: "test-aura-token"}).client(); status.Code(err) != codes.FailedPrecondition {
			t.Fatal("invalid address accepted", address)
		}
	}
	if client, err := (connectionConfig{Address: "10.0.0.10:8888", APIToken: "test-aura-token\r\n"}).client(); err != nil || client.apiToken != "test-aura-token" {
		t.Fatal("token with a trailing line break was rejected or not trimmed", err)
	}
	if _, err := (connectionConfig{Address: "10.0.0.10:8888", APIToken: "test\naura-token"}).client(); status.Code(err) != codes.FailedPrecondition {
		t.Fatal("token with an inner newline accepted", err)
	}
	server := &artworkServer{}
	if err := server.configure(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	response, err := server.GetImages(context.Background(), &pluginv1.GetImagesRequest{ItemType: "movie"})
	if err != nil || len(response.Images) != 0 {
		t.Fatal("missing identity should return no images without a network call", err)
	}
	if _, err := server.Search(context.Background(), &pluginv1.SearchMetadataRequest{}); status.Code(err) != codes.FailedPrecondition {
		t.Fatal("connection test should report missing configuration", err)
	}
}

func writeEnvelope(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "success", "data": data})
}

func TestSeasonPosterTypeAliases(t *testing.T) {
	season := int32(1)
	// Aura's code sends season_poster; its API docs list the other spellings.
	for _, assetType := range []string{"season_poster", "seasonPoster", "specialSeasonPoster", "special_season_poster"} {
		asset := auraImage{Type: assetType, ItemTMDBID: "123", SeasonNumber: &season}
		if kind := imageKind(asset, "123", &season); kind != "poster" {
			t.Errorf("season asset type %q: kind = %q; want poster", assetType, kind)
		}
	}
}
