# flashroute

Run the [FlashRoute](https://github.com/uiautodev/flashroute) AI gateway with `npx`:

```bash
npx flashroute
# open http://localhost:8080
```

The wrapper downloads the matching `flashroute-server-*` binary from the GitHub release for your platform, verifies its SHA-256 checksum, caches it under `~/.cache/flashroute`, and runs it. Arguments are passed through:

```bash
npx flashroute -listen 0.0.0.0:8080 -data-dir ~/.config/flashroute
npx flashroute -version
```

Set `FLASHROUTE_CACHE_DIR` to change the download cache location. Set `FLASHROUTE_RELEASE_BASE_URL` to use a release mirror or a local test server.
