package main

import (
	"context"
	_ "embed"
	"encoding/json"

	pluginv1 "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginproto/silo/plugin/v1"
	"github.com/Silo-Server/silo-plugin-sdk/pkg/pluginsdk/runtime"
)

//go:embed manifest.json
var manifestJSON []byte

var version = "0.1.0"

type artworkServer struct {
	pluginv1.UnimplementedMetadataProviderServer
	pluginv1.UnimplementedImageResolverServer
	configuration connectionConfig
}

func (s *artworkServer) configure(_ context.Context, entries []*pluginv1.ConfigEntry) error {
	// Silo configures once before capability calls and restarts on settings changes.
	s.configuration = connectionConfig{}
	for _, entry := range entries {
		if entry.GetKey() != "connection" {
			continue
		}
		data, err := json.Marshal(entry.GetValue().AsMap())
		if err != nil {
			return err
		}
		return json.Unmarshal(data, &s.configuration)
	}
	return nil
}

func (s *artworkServer) Search(ctx context.Context, _ *pluginv1.SearchMetadataRequest) (*pluginv1.SearchMetadataResponse, error) {
	client, err := s.configuration.client()
	if err != nil {
		return nil, err
	}
	// Silo uses Search for its connection test; this provider never identifies titles.
	if _, err := client.libraries(ctx); err != nil {
		return nil, err
	}
	return &pluginv1.SearchMetadataResponse{}, nil
}

func main() {
	server := &artworkServer{}
	runtime.ServeManifestWithOptions(manifestJSON, version, runtime.CapabilityServers{
		MetadataProvider: server,
		ImageResolver:    server,
	}, runtime.WithConfigure(server.configure))
}
