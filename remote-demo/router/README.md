# WAMP Router

This folder is intentionally minimal. Run any WAMP-compatible router that exposes:

- WebSocket endpoint: `ws://localhost:8080/ws`
- Realm: `default`

Example with Docker + Crossbar:

```bash
docker run --rm -p 8080:8080 -it crossbario/crossbar
```

Then point both the Go agent and Flutter viewer to the same URL/realm using env vars or dart-defines.
