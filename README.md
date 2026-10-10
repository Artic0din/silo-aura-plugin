# AURA Artwork Plugin for Silo

Community [Silo](https://github.com/Silo-Server/silo-server) metadata plugin
backed by [AURA](https://github.com/mediux-team/AURA). It adds MediUX movie and
series posters, backdrops, and season posters, including Specials, to Silo's
image picker and resolves `aura://` artwork references. Each image is labelled
**AURA** and shows the set creator's username. The plugin does not identify
titles or choose artwork during metadata refreshes.

## Requirements

- A Silo server with `image_picker_lookup_provider_ids` support, requested in
  [Silo-Server/silo-server#2198](https://github.com/Silo-Server/silo-server/issues/2198).
  An unmodified server only asks a provider for images when the title has that
  provider's own ID, which no title has for AURA; the extension lets Silo use
  the title's TMDB ID instead.
- A running AURA instance with configured movie and show libraries and a
  populated library cache.
- Titles that already have a TMDB ID in Silo.

## Setup

1. Download the binary for your server from
   [Releases](https://github.com/Artic0din/silo-plugin-metadata-aura/releases) and
   install it through **Admin > Plugins > Catalog > Install from a file**.
2. Open **AURA Artwork**, enter the **AURA address** and **API token**, select
   **Test connection**, then select **Save connection**.
3. Enable **AURA Artwork** in the movie and series libraries' metadata provider
   chains, including the season level for season posters.
4. Open a title's **Edit Metadata > Images** and choose **Apply** on an AURA
   image.

The address is a private IP with AURA's API port, such as `10.0.0.10:8888`, or
an HTTPS URL. Plain HTTP is accepted only for private or loopback addresses. Use
AURA's API port, not its web app port. Silo stores the API token as an encrypted
secret, and it never appears in an image URL.

## Known Limitations

- Picker previews load from AURA's address in the browser, so both the browser
  and Silo must reach it. When Silo is served over HTTPS, use an HTTPS AURA
  address; browsers block HTTP images on HTTPS pages.
- Applying an image changes the title immediately. Cancelling the editor does
  not undo it.
- Episode title cards are not offered, because Silo's image request has no
  episode number.
- Backdrops are listed without a language so Silo's **Textless** filter shows
  them, even when MediUX tagged one. Posters keep AURA's language because they
  usually carry title text.

## Dependency Model

This repository consumes `github.com/Silo-Server/silo-plugin-sdk` as a normal Go module dependency. CI and release builds run with `GOWORK=off` and expect the SDK version in `go.mod` to resolve from a published semver tag.

For local multi-repository development, use a `go.work` file that points at a
sibling SDK checkout. Do not commit machine-local filesystem replacements.

## Development

```sh
go test ./...
go build .
```

## Contributing

Read [CONTRIBUTING.md](CONTRIBUTING.md) before opening a pull request. Artwork
lookup, image filtering, image resolution, configuration, or advertised
capability changes should start as an issue.

## Attribution

Artwork is created by the [MediUX](https://mediux.pro/) community and served
through [AURA](https://github.com/mediux-team/AURA). This community plugin is
not maintained or endorsed by Silo, AURA, or MediUX.

<a href="https://mediux.pro/">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/mediux-team/AURA/e11b753746779be12013bd5277f931f4c809f05f/frontend/public/mediux_word_logo.svg">
    <img src=".github/assets/mediux-logo-light.svg" alt="MediUX Logo" width="200">
  </picture>
</a>
<a href="https://github.com/mediux-team/AURA">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/mediux-team/AURA/e11b753746779be12013bd5277f931f4c809f05f/frontend/public/aura_word_logo.svg">
    <img src=".github/assets/aura-logo-light.svg" alt="AURA Logo" width="128">
  </picture>
</a>

## License

`silo-plugin-metadata-aura` is licensed under `AGPL-3.0-or-later`. See [LICENSE](LICENSE).
