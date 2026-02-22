# vexShare

**Share your terminal session securely via browser.**

vexShare is a lightweight CLI tool that starts a local terminal (PTY) and makes it accessible through a browser via WebSocket. It supports password and token-based authentication, TLS, rate limiting, and multi-user viewing.

## Features

- **Secure by default** — binds to `127.0.0.1`, password-protected, secure cookies
- **Browser-based** — xterm.js terminal emulator, no client installation needed
- **Flexible auth** — password, token, or both (`password+token`)
- **Multi-user** — single-controller with read-only viewers (or shared input mode)
- **Optional TLS** — HTTPS with your own certificates
- **Rate limiting** — built-in per-IP rate limiting for login and WebSocket
- **Idle timeout** — automatic session cleanup after inactivity
- **Zero dependencies** — single binary, embedded web assets, just `go build`

## Quickstart

### Build

```bash
go build -o vexshare ./cmd/vexshare
```

### Run with password auth (default)

```bash
./vexshare
```

This starts bash on `127.0.0.1:8080` with a randomly generated password. Credentials are printed to the console:

```
  ┌─────────────────────────────────────────────┐
  │           vexShare — Terminal Sharing       │
  └─────────────────────────────────────────────┘

  Auth Mode    : password
  Username     : vex
  Password     : aB3x_kQm7pLnR2Wd
  URL          : http://127.0.0.1:8080
  Command      : bash
  Idle Timeout : 30m0s

  Press Ctrl+C to stop.
```

Open the URL in your browser, log in, and you're sharing your terminal!

### Run with a specific password

```bash
./vexshare --password "my-secret" --user admin
```

### Token-based access

```bash
./vexshare --auth token
```

Generates a secure token URL like `http://127.0.0.1:8080/t/aB3xkQm7pLnR2Wd.../`

### Combined password + token

```bash
./vexshare --auth password+token
```

Both `/login` (password) and `/t/{token}/` (token URL) are active.

### With TLS

```bash
./vexshare --tls-cert cert.pem --tls-key key.pem
```

### Custom command

```bash
./vexshare --cmd "htop"
./vexshare --cmd "python3"
./vexshare --cmd "ssh user@remote"
```

### Shared input (all clients can type)

```bash
./vexshare --shared-input
```

### Custom listen address

```bash
./vexshare --listen 0.0.0.0:9090
```

> **Warning:** Binding to `0.0.0.0` exposes vexShare to your network. Always use TLS and strong authentication when not on localhost.

## CLI Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--listen` | `127.0.0.1:8080` | Address to listen on |
| `--cmd` | `bash` | Command to run in PTY |
| `--auth` | `password` | Auth mode: `password`, `token`, `password+token` |
| `--user` | `vex` | Username for password auth |
| `--password` | *(auto-generated)* | Password for auth |
| `--token` | *(auto-generated)* | Access token |
| `--shared-input` | `false` | Allow all clients to write input |
| `--idle-timeout` | `30m` | Idle timeout before session shutdown |
| `--tls-cert` | | Path to TLS certificate (enables HTTPS) |
| `--tls-key` | | Path to TLS private key (enables HTTPS) |
| `--log-level` | `info` | Log level: `debug`, `info`, `warn`, `error` |
| `--allow-origin` | *(same-origin)* | Allowed origins for WebSocket |
| `--version` | | Print version and exit |

## HTTP Endpoints

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `GET` | `/` | Password | Terminal UI |
| `GET` | `/login` | — | Login page |
| `POST` | `/login` | — | Submit login |
| `POST` | `/logout` | — | Clear session |
| `GET` | `/ws` | Password | WebSocket endpoint |
| `GET` | `/healthz` | — | Health check |
| `GET` | `/t/{token}/` | Token | Token-protected terminal UI |
| `GET` | `/t/{token}/ws` | Token | Token-protected WebSocket |

## Multi-User Behavior

- **Single-controller mode** (default): The first connected client is the **controller** and has write access. Additional clients are **viewers** — they can see the terminal but cannot type.
- **Shared-input mode** (`--shared-input`): All connected clients can type.
- If the controller disconnects, the next connected client is promoted.

## Security

### Important Security Notes

1. **Always use localhost** unless you have TLS enabled and strong authentication configured.
2. **Use TLS** when exposing vexShare beyond localhost — `--tls-cert` and `--tls-key`.
3. **Use strong passwords** or let vexShare auto-generate them.
4. **Token URLs are secrets** — treat them like passwords.
5. **Rate limiting** is built-in (5 login attempts/min, 20 WS connections/min per IP).
6. **Cookies** are `HttpOnly`, `SameSite=Lax`, and `Secure` when TLS is enabled.
7. **Don't expose to the internet** without understanding the risks.

### How it works

```
┌─────────┐     WebSocket      ┌──────────┐     PTY      ┌─────────┐
│ Browser  │ ◄──────────────► │ vexShare  │ ◄──────────► │  bash   │
│ (xterm)  │                   │  server   │              │ (or cmd)│
└─────────┘                    └──────────┘              └─────────┘
```

## Development

### Prerequisites

- Go 1.22+
- Linux or macOS (PTY support required)

### Build

```bash
go build -o vexshare ./cmd/vexshare
```

### Build with version

```bash
go build -ldflags "-X main.Version=1.0.0" -o vexshare ./cmd/vexshare
```

### Run tests

```bash
go test ./...
```

### Run in development

```bash
go run ./cmd/vexshare --log-level debug
```

### Project structure

```
vexSHARE/
├── cmd/
│   └── vexshare/
│       └── main.go
├── internal/
│   ├── auth/
│   │   ├── auth.go
│   │   └── auth_test.go
│   ├── ratelimit/
│   │   ├── ratelimit.go
│   │   └── ratelimit_test.go
│   ├── tokens/
│   │   ├── tokens.go
│   │   └── tokens_test.go
│   ├── session/
│   │   └── session.go
│   ├── server/
│   │   └── server.go
│   └── ui/
│       ├── ui.go
│       └── static/
│           ├── login.html
│           └── terminal.html
├── go.mod
├── go.sum
├── LICENSE
└── README.md
```

## License

MIT — see [LICENSE](LICENSE).
