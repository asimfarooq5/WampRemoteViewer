# Remote Demo

Minimal TeamViewer-style prototype:

- `agent/`: Go desktop agent using `xconn-go`, `screenshot`, `robotgo`
- `viewer/`: Flutter viewer using `xconn-dart`
- `router/`: Notes for running any WAMP router

## Protocol

- Topic: `io.xconn.desktop.frame`
- RPC:
  - `io.xconn.desktop.start`
  - `io.xconn.desktop.stop`
  - `io.xconn.desktop.mouse`
  - `io.xconn.desktop.key`

## Behavior

- Agent captures display, resizes frames, JPEG encodes at quality `60`, publishes at `10 FPS`.
- Viewer subscribes to frame topic, renders using `Image.memory`, forwards taps as mouse RPC and keyboard events as key RPC.
