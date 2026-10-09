package main

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	pluginv1 "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginproto/silo/plugin/v1"
	"github.com/Silo-Server/silo-plugin-sdk/pkg/pluginsdk/imagevariant"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

const artworkScheme = "aura://"

var assetIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,160}$`)

type auraImage struct {
	ID            string `json:"id"`
	Type          string `json:"type"`
	Modified      string `json:"modified"`
	Language      string `json:"language"`
	ItemTMDBID    string `json:"item_tmdb_id"`
	SeasonNumber  *int32 `json:"season_number"`
	EpisodeNumber *int32 `json:"episode_number"`
}

type auraSet struct {
	UserCreated string      `json:"user_created"`
	Images      []auraImage `json:"images"`
}

func (s *artworkServer) GetImages(ctx context.Context, req *pluginv1.GetImagesRequest) (*pluginv1.GetImagesResponse, error) {
	response := &pluginv1.GetImagesResponse{}
	itemType := ""
	switch req.GetItemType() {
	case "movie":
		itemType = "movie"
	case "series":
		itemType = "show"
	default:
		return response, nil
	}
	tmdbID := req.GetProviderIds().GetFields()["tmdb"].GetStringValue()
	if tmdbID == "" {
		return response, nil
	}
	if id, err := strconv.ParseUint(tmdbID, 10, 64); err != nil || id == 0 {
		return nil, status.Error(codes.InvalidArgument, "Artwork lookup needs a positive TMDB ID.")
	}
	if req.SeasonNumber != nil && (itemType != "show" || req.GetSeasonNumber() < 0) {
		return nil, status.Error(codes.InvalidArgument, "Season artwork needs a series and a nonnegative season number.")
	}
	client, err := s.configuration.client()
	if err != nil {
		return nil, err
	}
	libraryTitle, err := client.libraryTitle(ctx, itemType)
	if err != nil {
		return nil, err
	}
	var data struct {
		Sets *[]auraSet `json:"sets"`
	}
	if err := client.get(ctx, "/api/mediux/sets/item", url.Values{
		"tmdb_id": {tmdbID}, "item_type": {itemType}, "item_library_title": {libraryTitle},
	}, &data); err != nil {
		if errors.Is(err, errNoArtwork) {
			return response, nil
		}
		return nil, err
	}
	if data.Sets == nil {
		return nil, status.Error(codes.DataLoss, "Aura returned no artwork sets field.")
	}
	return imagesFromSets(*data.Sets, tmdbID, req.SeasonNumber)
}

func imagesFromSets(sets []auraSet, tmdbID string, season *int32) (*pluginv1.GetImagesResponse, error) {
	response := &pluginv1.GetImagesResponse{}
	seen := make(map[string]bool)
	for _, set := range sets {
		for _, asset := range set.Images {
			kind := imageKind(asset, tmdbID, season)
			if kind == "" {
				continue
			}
			if !assetIDPattern.MatchString(asset.ID) || !validModifiedDate(asset.Modified) {
				return nil, status.Error(codes.DataLoss, "Aura returned an artwork asset with an invalid ID or modified date.")
			}
			path := artworkScheme + "image/" + asset.ID + "?" + url.Values{"modified_date": {asset.Modified}}.Encode()
			if seen[path] {
				continue
			}
			seen[path] = true
			response.Images = append(response.Images, &pluginv1.ImageRecord{
				Kind: kind, Url: path, Language: asset.Language, SeasonNumber: season,
				Metadata: &structpb.Struct{Fields: map[string]*structpb.Value{
					"creator": structpb.NewStringValue(strings.TrimSpace(set.UserCreated)),
				}},
			})
		}
	}
	return response, nil
}

func imageKind(asset auraImage, tmdbID string, season *int32) string {
	if asset.ItemTMDBID != tmdbID || asset.EpisodeNumber != nil {
		return ""
	}
	if season != nil {
		if asset.SeasonNumber == nil || *asset.SeasonNumber != *season {
			return ""
		}
		switch asset.Type {
		case "seasonPoster", "season_poster", "specialSeasonPoster", "special_season_poster":
			return "poster"
		}
		return ""
	}
	if asset.SeasonNumber != nil {
		return ""
	}
	if asset.Type == "poster" || asset.Type == "backdrop" {
		return asset.Type
	}
	return ""
}

func validModifiedDate(value string) bool {
	_, err := time.Parse(time.RFC3339Nano, value)
	return err == nil
}

func (s *artworkServer) resolveImage(rawPath, variant string) (string, error) {
	client, err := s.configuration.client()
	if err != nil {
		return "", err
	}
	path, err := url.Parse(strings.TrimPrefix(rawPath, artworkScheme))
	if err != nil || path.IsAbs() || path.Host != "" || path.Fragment != "" || !strings.HasPrefix(path.Path, "image/") {
		return "", status.Error(codes.InvalidArgument, "Invalid Aura artwork path.")
	}
	assetID := strings.TrimPrefix(path.Path, "image/")
	query, err := url.ParseQuery(path.RawQuery)
	if err != nil || len(query) != 1 || len(query["modified_date"]) != 1 || !assetIDPattern.MatchString(assetID) || !validModifiedDate(query.Get("modified_date")) {
		return "", status.Error(codes.InvalidArgument, "Invalid Aura artwork ID or modified date.")
	}
	quality := "original"
	switch variant {
	case imagevariant.Card:
		quality = "thumb"
	case imagevariant.Featured, imagevariant.Large:
		quality = "optimized"
	}
	u := client.baseURL.JoinPath("/api/images/mediux/item")
	u.RawQuery = url.Values{
		"asset_id": {assetID}, "modified_date": {query.Get("modified_date")}, "quality": {quality},
	}.Encode()
	return u.String(), nil
}

func (s *artworkServer) ResolveImageURL(_ context.Context, req *pluginv1.ResolveImageURLRequest) (*pluginv1.ResolveImageURLResponse, error) {
	u, err := s.resolveImage(req.GetPath(), req.GetVariant())
	if err != nil {
		return nil, err
	}
	return &pluginv1.ResolveImageURLResponse{Url: u}, nil
}

func (s *artworkServer) ResolveImageURLs(_ context.Context, req *pluginv1.ResolveImageURLsRequest) (*pluginv1.ResolveImageURLsResponse, error) {
	urls := make(map[string]string, len(req.GetPaths()))
	for _, path := range req.GetPaths() {
		u, err := s.resolveImage(path, req.GetVariant())
		if err != nil {
			return nil, err
		}
		urls[path] = u
	}
	return &pluginv1.ResolveImageURLsResponse{Urls: urls}, nil
}
