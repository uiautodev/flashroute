# FlashRoute Desktop

The desktop wrapper uses Wails v3. It serves the embedded SvelteKit build from the
same Go HTTP handler as the server, so API calls and routing behave identically.
On macOS, the Wails v3 asset server streams chat responses.

Build the embedded frontend first, then build the universal macOS app:

```sh
make frontend
make desktop
```

The app is written to `desktop/build/bin/FlashRoute.app`.
Localhost and desktop sessions never require a password.
