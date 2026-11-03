# bifrost-merge-system-messages

A [Bifrost](https://github.com/maximhq/bifrost) image with one extra native plugin,
`merge-leading-system-messages`, published as `ghcr.io/pfrybar/bifrost:v<version>`.

## Why

Qwen 3.5+ chat templates (including Qwen 3.8) allow only one system message and fail
with `System message must be at the beginning.` when a client such as Open WebUI sends
several. The plugin merges consecutive leading system messages in Chat Completions
requests before Bifrost forwards them:

```text
system: A                    system: A\n\nB
system: B          →         user:   hello
user:   hello                system: C
system: C
```

The change is deliberately narrow. A request is left unchanged if it is not a `POST` to
a path ending in `/chat/completions`, has fewer than two leading system messages, has
non-string (e.g. multimodal) system content, or is not valid JSON. Fields on the first
system message are preserved, and system messages later in the conversation are not
touched (a later one will still trip the Qwen template). `/v1/responses` and
provider-native request formats are not modified.

The merge runs in two places:

- **`HTTPTransportPreHook`** merges the system messages the client sent, editing only
  the `messages` field of the raw body with `gjson`/`sjson`.
- **`PreLLMHook`** merges again after Bifrost core has built the final message list.
  This is needed because core adds a system message of its own: when an MCP server
  configured in Bifrost provides `instructions`, Bifrost prepends them as a separate
  system message (`<mcp_server name="…">…</mcp_server>`) ahead of the client's. That
  happens after the HTTP hooks run, so only an LLM hook can see it. The merged message
  keeps Bifrost's MCP instructions first and the client's instructions last.

## Layout

```text
plugin/                      Go plugin source and tests
scripts/patch-dockerfile.py  Makes upstream's Dockerfile.local plugin-capable
.github/workflows/build-image.yml
```

## How the image is built

Upstream's official image cannot load plugins: its binary is statically linked, and Go
only loads `.so` plugins into a dynamically linked binary built with the same Go version,
dependency versions, flags and libc. So the workflow builds Bifrost from source together
with the plugin:

1. Clone upstream `maximhq/bifrost` at tag `transports/v<version>` (the gateway release
   number shown in the Bifrost UI and used for upstream's Docker tags, not the `core`
   module version).
2. Copy `plugin/` into the clone as `plugins/mergeleadingsystemmessages/`. Upstream's
   Dockerfile adds every `plugins/*/go.mod` to its Go workspace, so the plugin builds
   against that release's own `core/`.
3. Run `scripts/patch-dockerfile.py` on upstream's `transports/Dockerfile.local` to
   build Bifrost dynamically, build the plugin in the same step, add `sqlite-libs` to
   the runtime image, and copy the plugin to
   `/app/plugins/merge-leading-system-messages.so`. Each edit checks for the expected
   upstream text first and fails loudly if upstream has changed the file.
4. Build and push `ghcr.io/pfrybar/bifrost:v<version>` for `linux/amd64`.

Everything else in the image (base images, UI, entrypoint, user, healthcheck) is
upstream's.

## Publishing a release

**Automatic:** the workflow runs daily (06:17 UTC), finds upstream's newest stable
`transports/v<version>` tag (prereleases are skipped), and builds it if
`ghcr.io/pfrybar/bifrost:v<version>` doesn't exist yet. If a release can't be built
(for example upstream changed `Dockerfile.local` and the patch script refuses it), the
run fails and GitHub emails you; it will retry every day until fixed.

**Manual:** **Actions → Build custom Bifrost image → Run workflow**. Leave the version
empty for the newest release, or enter one without the `v` (e.g. `2.4.0`). Manual runs
always build, overwriting an existing tag, which is how to republish after a plugin
change.

The plugin tests run first; the image is only built if they pass. Pushes and pull
requests run the tests only.

GitHub disables scheduled workflows in a public repository after 60 days without
commits; re-enable it from the Actions tab if that happens.

The workflow pushes with the repository's `GITHUB_TOKEN`, so this repository needs
write access to the `ghcr.io/pfrybar/bifrost` package (package settings → **Manage
Actions access**).

When moving to a new upstream release, also bump `plugin/go.mod` to that release's Go
version and `github.com/maximhq/bifrost/core` version (see upstream's
`transports/go.mod`) so the standalone tests match. The image build itself always uses
the release's own `core/`.

## Configuration

Enable the plugin in Bifrost's `config.json`:

```json
{
  "plugins": [
    {
      "name": "merge-leading-system-messages",
      "enabled": true,
      "path": "/app/plugins/merge-leading-system-messages.so",
      "version": 1,
      "placement": "pre_builtin",
      "order": 0
    }
  ]
}
```

`pre_builtin` with `order: 0` runs it before the built-in plugins. With DB-backed
configuration, increment `version` whenever the plugin binary changes so Bifrost
reloads it. On startup the logs should show
`plugin status: merge-leading-system-messages - active`.

For Helm, override the chart's image:

```yaml
image:
  repository: ghcr.io/pfrybar/bifrost
  tag: v2.4.0
```

## Local development

```bash
# Plugin tests (Go version from plugin/go.mod)
cd plugin && go test ./...

# Full image build, same as CI
git clone --depth 1 --branch transports/v2.4.0 https://github.com/maximhq/bifrost.git /tmp/upstream
cp -a plugin /tmp/upstream/plugins/mergeleadingsystemmessages
python3 scripts/patch-dockerfile.py /tmp/upstream/transports/Dockerfile.local /tmp/upstream/transports/Dockerfile.custom
docker build -f /tmp/upstream/transports/Dockerfile.custom --build-arg VERSION=2.4.0 -t bifrost-custom:v2.4.0 /tmp/upstream
```
