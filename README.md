# go-rfb

A Go implementation of the RFB (Remote Framebuffer) / VNC protocol, extended with
support for the ATEN iKVM (IPMI/BMC KVM-over-IP) private protocol and a
transport-agnostic proxy for bridging any VNC client (e.g. noVNC) to an ATEN iKVM
session.

It can act as:

* an **RFB client** that connects to a standard VNC server, or to an ATEN iKVM
  endpoint (with ATEN's private authentication and hermon-encoded framebuffer),
* an **RFB server** that a VNC client can connect to, and
* a **proxy** (see the [`proxy`](./proxy) package) that takes a plain `net.Conn`
  on each side — a standard RFB server on the downstream side and an ATEN iKVM
  client on the upstream side — and transcodes on the fly.

## Protocol reference

The authoritative RFB protocol specification is
[rfbproto](https://github.com/rfbproto/rfbproto/blob/master/rfbproto.rst). All
standard encodings implemented here are written against that document. The ATEN
private encodings (hermon and friends) are not part of rfbproto and are
documented by this repository's own handling of them.

## Encoding support

Standard RFB encodings (per rfbproto):

| Encoding        | Value  | Read | Write | Notes |
|-----------------|--------|:----:|:-----:|-------|
| Raw             | 0      | ✅   | ✅    | |
| CopyRect        | 1      | ✅   | ✅    | |
| RRE             | 2      | ✅   | ✅    | Out-of-bounds sub-rects are clamped. |
| Hextile         | 5      | ✅   | ✅    | All five sub-rect modes. Raw tiles carry `tile_w × tile_h` pixels (spec: "the width and height of the tile"), so edge tiles are shorter. |
| Tight           | 7      | ⚠️   | ⚠️    | Stub: `Read`/`Write` are no-ops (no bytes consumed). A real Tight rect would fill with the background colour without desyncing the stream. |
| Rich Cursor     | -239   | ✅   | ✅    | Pixels are decoded; the alpha/and-mask application is not implemented. |
| X Cursor        | -240   | ✅   | ✅    | |
| DesktopSize pseudo | -223 | ✅   | ✅    | |
| DesktopName pseudo | -307 | ✅   | ✅    | |
| LedState pseudo | -261   | ✅   | ✅    | |

Internal / ATEN encodings (not part of rfbproto):

| Encoding   | Value | Direction | Notes |
|------------|-------|-----------|-------|
| TightPng   | -260  | client ← server | Our own re-encoding used by the proxy to send tight (PNG/Fill) rectangles to clients that advertised the encoding. |
| ATEN Hermon| 0x59  | client ← iKVM | ATEN's proprietary framebuffer encoding; read and transcoded to Raw. |

Not implemented (the `EncodingType` constants exist, but the decoder in
`image.go` reports "unsupported encoding" for them): Zlib, ZlibHex, CoRRE,
Ultra1, Ultra2, JPEG, JRLE, TRLE, ZRLE, and the remaining ATEN encodings
(AST2100, ASTJPEG, Yarkon, Pilot3). These are not needed for the ATEN iKVM
upstream, which speaks raw/hermon.

## Usage

The repository ships three runnable examples in [`example/`](./example):

* [`example/client`](./example/client) — an RFB client that dials a server (a
  standard VNC server, or an ATEN iKVM with `ClientAuthATEN`) and dispatches
  server messages to a channel.
* [`example/server`](./example/server) — an RFB server that a VNC client can
  connect to.
* [`example/proxy`](./example/proxy) — a proxy bridging VNC clients to an ATEN
  iKVM session.

### The proxy

The [`proxy`](./proxy) package is transport-agnostic and built entirely on plain
`net.Conn` values. One side is a standard RFB server that any VNC client can
connect to; the other is an RFB client connected to an ATEN iKVM session.
Hermon-encoded frames from the iKVM are transcoded on the fly into standard RFB
rectangles, and for clients that advertised the `TightPng` encoding the
rectangles are re-encoded as tight (PNG/Fill) so such clients receive a normal
tight stream.

The package deliberately contains **no WebSocket code**: the consumer decides how
a WebSocket (e.g. for noVNC) becomes a `net.Conn`, and hands that connection to
the proxy. See the package doc in
[`proxy/proxy.go`](./proxy/proxy.go) and the example for the intended wiring.

## Building and testing

```sh
go build ./...
go test ./... -race -count=1
gofmt -l .
```

## License

MIT — see [LICENSE](./LICENSE).
