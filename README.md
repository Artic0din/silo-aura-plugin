# Aura Artwork for Silo

Choose MediUX posters and backdrops through [Aura](https://github.com/mediux-team/AURA) in Silo's existing image picker.
The plugin supports movies, series and exact season posters, including Specials.
Each choice is labelled **AURA** and shows the set uploader's username when Aura supplies it.
It does not identify titles or select artwork during metadata refreshes.

## Server requirement

Silo needs the `image_picker_lookup_provider_ids` extension tracked in [issue #1](https://github.com/Artic0din/silo-plugin-metadata-aura/issues/1).
An unmodified server skips an artwork-only provider because the title has no provider-specific Aura ID.
The plugin uses the published Silo SDK without changing its protobuf contract.

## Configure and use

1. Install the binary for your server's operating system and architecture through **Admin > Plugins > Catalog > Install from a file**.
2. Open **Aura Artwork** and enter the **Aura address** and **API token**.
3. Select **Test connection**, save the settings, and enable **Aura Artwork** in the movie and series libraries' provider priorities, including the season level for season posters.
4. Open a title's **Edit Metadata > Images** and choose **Apply** on an Aura image.

The address accepts a private IP and API port, such as `10.0.0.10:8888`, or an HTTPS URL.
HTTP is restricted to private or loopback IP addresses.
Use Aura's API port rather than its web app port.
The API token is a secret field saved and encrypted by Silo; it is never included in an image URL.
Aura's libraries are discovered automatically, so no library-name settings are needed.
Titles must already have a TMDB ID in Silo so Aura can find their artwork.
Aura must have configured movie/show libraries and a populated library cache.
Picker previews load from Aura's address in the browser; Silo downloads the original when artwork is applied and keeps its own copy.
Both the browser and Silo must be able to reach that address.
When Silo is opened over HTTPS, use an HTTPS Aura API address because browsers block HTTP image previews on HTTPS pages.
Applying an image changes the title immediately; cancelling the editor does not undo it.

Episode title cards are excluded because Silo's current image request has no episode identifier.
Assets belonging to other movies or series are filtered out.
Backdrops are listed without a language so Silo's **Textless** filter shows them, even when MediUX tagged one.
Posters keep Aura's language because they usually carry title text.
The picker uses thumbnail images, detail variants use optimized images, and full/original or unknown variants use the original.
The plugin reports authentication, cache and API failures separately from a title with no artwork.

## Build and check

```sh
go test -race ./...
go vet ./...
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o dist/silo-plugin-metadata-aura-linux-amd64 .
```

Use `GOARCH=arm64` for a Linux ARM server, or `GOOS=darwin GOARCH=arm64` for an Apple Silicon Mac.
Builds resolve `github.com/Silo-Server/silo-plugin-sdk` from its pinned published tag.
No machine-specific SDK replacement is committed.
`manifest.json` is embedded in the binary, and the SDK supplies binary introspection and its actual checksum.

## Contributing and releases

Read [CONTRIBUTING.md](CONTRIBUTING.md) and [AGENTS.md](AGENTS.md) before making changes.
They apply Silo's shared contribution rules and AI disclosure policy.
The reusable Linux CI workflow runs formatting, race tests, vet, build and manifest introspection against published dependencies.
The release workflow follows the TMDB plugin's three-platform binary and checksum layout.
Each GitHub release has notes generated from its merged pull requests, which the catalog links as the changelog.
Optional catalog notifications target [your Silo catalog fork](https://github.com/Artic0din/silo-plugins).
Publishing to Silo's official catalog remains a separate contribution.

## Attribution

Artwork is supplied by MediUX creators through Aura.
This community plugin is not maintained by Silo or the Aura/MediUX teams.
See [LICENSE](LICENSE) for the AGPL-3.0-or-later license.
