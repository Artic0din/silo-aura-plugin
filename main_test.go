package main

import (
	"testing"

	"github.com/Silo-Server/silo-plugin-sdk/pkg/pluginsdk/manifest"
)

func TestManifestMeetsCatalogPresentationContract(t *testing.T) {
	pluginManifest, err := manifest.Load(manifestJSON)
	if err != nil {
		t.Fatal(err)
	}
	if err := manifest.ValidateCatalogPresentation(pluginManifest, "https://github.com/Artic0din/silo-aura-plugin"); err != nil {
		t.Fatal(err)
	}
}
