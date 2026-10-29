# FlashRoute Desktop

The desktop wrapper uses Wails v2. It serves the embedded SvelteKit build from the
same Go HTTP handler as the server, so API calls and routing behave identically.

Build the embedded frontend first, then run Wails from this directory:

```sh
make frontend
cd desktop
wails build
```

The app is written to `desktop/build/bin/`.
Localhost and desktop sessions never require a password.
