# FlashRoute

A compact AI gateway and chat console built with Go and SvelteKit. It aggregates DeepSeek, OpenRouter, BigModel, Anthropic and any OpenAI-compatible provider, with HTTP/SOCKS proxies, ordered route aliases, automatic 429 fallback, usage stats, client API keys, reversible message redaction and image-capable chat.

English | [简体中文](README.zh-CN.md)

[![download](https://img.shields.io/github/v/release/uiautodev/flashroute?label=download&style=flat-square)](https://github.com/uiautodev/flashroute/releases/latest)

![FlashRoute home](docs/home.png)

## Features

- **Multi-provider aggregation**: DeepSeek, OpenRouter, BigModel, Anthropic and custom OpenAI-compatible endpoints.
- **Per-provider proxy**: `http://`, `https://`, `socks5://` or `socks5h://`, with an independent proxy connectivity test.
- **Provider views and balances**: switch between card and list views. DeepSeek and OpenRouter balances / remaining limits can be queried directly.
- **Provider models**: sync candidates from `/v1/models`, keep every model visible, toggle selection from the model chips, and test individual models with `1+1=? Only output the number.` Connection tests use the first model by default.
- **API types**: Chat Completions, Responses or Anthropic Messages. Requests and streaming responses are converted automatically.
- **Ordered route aliases**: for example `grounder` tries `ds/deepseek-flash`, then `aliyun/qwen3.8-27b`, falling back on 429, 5xx and network errors.
- **Usage stats**: route request counts, success rate, per-target hit rate and 429 counts, persisted to YAML.
- **Client keys**: generated with the `sk-fr-` prefix, masked by default in the UI and copyable with one click. Localhost requests can skip the gateway key by default; toggle this on the **API Keys** page.
- **Message redaction**: regex rules replace secrets in outbound messages before they reach a provider. Matches become same-length random values, keeping punctuation, whitespace and PEM markers, so a key like `sk-123456` still looks like a key, private keys keep their shape and email addresses keep the domain after `@`. Everything is restored in model replies; chat messages with sensitive content get a **Safe** badge. Text inside regex capture groups is preserved and the rest of each match is masked with the same length; hit counts and the last 30 days of replacement records are available on the **Data redaction** page and can be cleared immediately. Rules and the global switch also live there.
- **OpenAI-compatible gateway**: `/v1/models` and `/v1/chat/completions`, including `stream: true`.
- **Chat UI**: streaming output, reasoning toggle, text + images, Markdown, one-click copy, privacy mode (never written to disk), conversation history with delete/clear, and optional web search (on by default).
- **Per-message versions**: editing a user message adds a user-message version; regenerating an assistant reply adds an assistant-reply version. Each message shows its own `current/total` selector. The version tree is stored in SQLite.
- **Languages and themes**: Chinese / English, dark / light / follow system.
- **Access control**: localhost and desktop sessions need no password; remote access must set one. Use `-require-auth` behind a local reverse proxy.
- **Single binary**: SvelteKit assets are embedded with `go:embed`.
- **Desktop app**: optional Wails v3 shell reusing the same Go HTTP handler and frontend; it serves the OpenAI-compatible gateway on `http://127.0.0.1:11211` by default (configurable), and macOS streaming is supported through the Wails v3 asset server.
- **Agent integrations**: detect Claude Code and OpenCode, then apply the selected chat model with one click. Claude Code is pointed at the selected provider's Anthropic endpoint; OpenCode gets FlashRoute as an OpenAI-compatible provider.
- **In-app updates**: check the latest GitHub release from Settings, verify the downloaded asset with `SHA256SUMS`, install it and restart automatically. macOS desktop builds update the app bundle and re-sign it ad hoc.

## Alternatives

FlashRoute focuses on a lightweight, single-binary, self-hosted gateway with a built-in chat console. If you need a fuller billing stack, agent configuration management or an enterprise gateway, these projects are good references:

- [magpie](https://github.com/yetone/magpie) — local gateway and agent model switcher for Claude Code / Codex and more, with privacy masking, routing groups and subscription account pools.
- [LiteLLM](https://github.com/BerriAI/litellm) — mature Python LLM gateway / SDK with a large provider catalog, OpenAI-compatible proxy, budgets and rate limits.
- [New API](https://github.com/QuantumNous/new-api) — self-hosted LLM gateway with channels, tokens, billing and a user system.
- [One API](https://github.com/songquanpeng/one-api) — OpenAI-compatible aggregation gateway with multi-provider and token management.
- [Portkey Gateway](https://github.com/Portkey-AI/gateway) — production AI gateway with routing, caching, retries and guardrails.
- [Bifrost](https://github.com/maximhq/bifrost) — high-performance Go AI gateway with multi-provider fallback and observability.
- [Envoy AI Gateway](https://github.com/envoyproxy/ai-gateway) — Envoy-based AI traffic gateway for Kubernetes and platform teams.
- [Higress](https://github.com/alibaba/higress) — cloud-native API gateway with AI proxy and multi-model routing.
- [Helicone](https://github.com/Helicone/helicone) — LLM observability and proxy with caching, cost analytics and experiments.
- [OpenRouter](https://openrouter.ai) — hosted multi-model aggregation service, not a self-hosted project.

## Development

The server requires Go 1.27.3+ and Node.js 22.17+. The optional desktop module uses [Wails v3](https://wails.io/).

```bash
# Build the frontend and the single server binary
make build
# Output: bin/flashroute

./bin/flashroute
# Open http://localhost:8080
```

Development mode (Vite HMR + Go backend):

```bash
make dev
# Frontend: http://localhost:5173
# Backend:  http://127.0.0.1:8080
# Vite proxies /api and /v1 to the backend
```

Custom ports or data directory:

```bash
DEV_BACKEND_PORT=8081 DEV_FRONTEND_PORT=5174 make dev
FLASHROUTE_DATA_DIR=/tmp/flashroute-dev make dev
```

Frontend edits hot-reload. Go edits trigger an automatic rebuild and backend restart; the backend log is written to `flashroute-dev-backend.log` in the temporary directory.

Install the pre-commit hook once:

```bash
pre-commit install
pre-commit run --all-files
```

The hook checks `gofmt`, runs `go vet`, `go test ./...` and builds the frontend.

### Server mode

```bash
./bin/flashroute \
  -listen 0.0.0.0:8080 \
  -data-dir ~/.config/flashroute
```

The first visit from a **remote address** asks you to set a password. Requests from `localhost` / `127.0.0.1` stay password-free. Behind a local reverse proxy the proxy's requests look like loopback; add `-require-auth`:

```bash
./bin/flashroute -listen 0.0.0.0:8080 -require-auth
```

Other flags:

```bash
./bin/flashroute -version
./bin/flashroute -listen 127.0.0.1:9000 -data-dir ./data
FLASHROUTE_DATA_DIR=/path/to/data ./bin/flashroute
```

### Desktop (Wails)

```bash
make frontend
make desktop
# Output: desktop/build/bin/flashroute.app
```

`make desktop` runs `wails build`. Wails embeds the same frontend and Go HTTP handler; desktop sessions are always password-free.

## Data directory

The default is `~/.config/flashroute/`:

```text
~/.config/flashroute/
├── config.yaml   # providers, models, routes, API keys, password hash
├── stats.yaml    # route and target stats
├── flashroute.db # non-private chats, message tree and redaction records (pure-Go SQLite)
└── uploads/      # non-private chat images
```

Provider API keys are stored in plain text in `config.yaml` and masked by default in the UI; use the copy button to get the full value. Files are created with `0600` and the data directory with `0700`. Chats use pure-Go SQLite; editing a user message adds a user-message version, and regenerating an assistant reply adds a reply version. An existing `chats.yaml` is migrated automatically, and a legacy `chats.db` is renamed to `flashroute.db` on startup.

## Using the gateway

Run it directly from npm:

```bash
npx flashroute
```

The npm wrapper downloads the matching release binary, verifies its SHA-256 checksum and forwards all flags. `flashroute -version` prints the version and git commit.

Create or copy an `sk-fr-` key on the **API Keys** page, then:

```bash
curl http://localhost:8080/v1/models \
  -H "Authorization: Bearer sk-fr-..."

curl http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer sk-fr-..." \
  -H "Content-Type: application/json" \
  -d '{
    "model": "grounder",
    "messages": [{"role": "user", "content": "hello"}],
    "stream": true
  }'
```

The model name can be:

- a route alias, such as `grounder`
- a direct model, formatted as `<provider-id>/<model-id>`, such as `deepseek/deepseek-chat`

Response headers expose the actual target:

```text
X-FlashRoute-Alias
X-FlashRoute-Provider
X-FlashRoute-Model
X-FlashRoute-Attempts
```

## In-app updates

Open **Settings → Software update** and click **Check for updates**. When a newer release exists, FlashRoute downloads the matching asset (server binary or macOS DMG), verifies it against the release's `SHA256SUMS`, replaces the running executable and restarts automatically. The restarted process serves the new version; macOS desktop builds also update the bundle metadata and re-sign the app ad hoc.

To use a GitHub mirror or a local release API:

```bash
FLASHROUTE_UPDATE_API=https://api.github.com/repos/uiautodev/flashroute/releases/latest \
FLASHROUTE_UPDATE_BASE_URL=https://mirror.example/flashroute/releases/download \
./flashroute
```

`FLASHROUTE_UPDATE_API` returns the latest release JSON. `FLASHROUTE_UPDATE_BASE_URL` must expose assets at `v<version>/<asset-name>` and is also used for `SHA256SUMS`. Standard `HTTP_PROXY` / `HTTPS_PROXY` environment variables are respected.

## Chat web search

The chat header has a **Web search** switch, on by default. When enabled, the model gets `web_search` and `web_fetch` function tools and decides when to call them. If a DeepSeek provider is configured, the tools use the DeepSeek Search / Fetch APIs; otherwise FlashRoute falls back to Bing search and direct page fetching. The current date and time are included in the tool prompt. When redaction is enabled, queries are redacted before they leave FlashRoute. Set `FLASHROUTE_SEARCH_URL` to use a compatible Bing or DuckDuckGo HTML endpoint or mirror for the fallback search.

## Agent integrations

Open the **Agents** page to detect locally installed coding agents. Choose a model or route alias from the picker and click **Apply**:

- **OpenCode** gets a `flashroute` provider pointing at the FlashRoute `/v1` gateway, plus the selected model in its model list.
- **Claude Code** is pointed at the selected provider's Anthropic endpoint with that provider's own key and model.

Connected agents are marked in the UI, and **Disconnect** removes the FlashRoute provider/environment again. Existing configs are backed up next to the original file before writing. Only detected agents can be applied.

## Project layout

```text
cmd/flashroute/        server entry point
desktop/               optional Wails v3 desktop entry (separate Go module)
frontend/              SvelteKit frontend; build/ is embedded with go:embed
internal/config/       config data structures
internal/store/        config, stats and chat persistence
internal/gateway/      upstream requests, proxies, model tests, fallback routing
internal/server/       HTTP API, auth, gateway and static files
```

## Testing

```bash
make test
# or build the frontend first, then run Go tests
make frontend
go test ./...
```

The UI is tested in a real browser with Playwright, covering provider setup, model sync and tests, proxy tests, 429 fallback, Markdown chat, message editing and version switching, images, privacy mode, API key copying, themes, language switching and remote-password flows.

## Release

Push a semver tag to trigger the release workflow:

```bash
git tag v1.2.3
git push origin v1.2.3
```

GitHub Actions builds the frontend, cross-compiles the server binaries, builds a universal macOS DMG (`FlashRoute-desktop-mac-<version>.dmg`), uploads everything with a `SHA256SUMS` file to the GitHub Release, and publishes the matching npm package when the `NPM_TOKEN` repository secret (an npm automation token) is configured. The package is published as `flashroute`. Binary versions use the tag without the leading `v`, and the commit hash is embedded into the binary and shown on the Settings page.

## License

[MIT](LICENSE)
