package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/vextm/vexshare/internal/auth"
	"github.com/vextm/vexshare/internal/server"
	"github.com/vextm/vexshare/internal/session"
	"github.com/vextm/vexshare/internal/tokens"
)

var Version = "dev"

func main() {
	if runtime.GOOS == "windows" {
		fmt.Fprintln(os.Stderr, "Error: vexShare requires PTY support and does not run on Windows.")
		fmt.Fprintln(os.Stderr, "Please use Linux or macOS.")
		os.Exit(1)
	}

	listen := flag.String("listen", "127.0.0.1:8080", "address to listen on")
	cmd := flag.String("cmd", "bash", "command to run in PTY")
	authMode := flag.String("auth", "password", "auth mode: password, token, password+token")
	user := flag.String("user", "vex", "username for password auth")
	password := flag.String("password", "", "password (auto-generated if empty)")
	token := flag.String("token", "", "access token (auto-generated if empty)")
	sharedInput := flag.Bool("shared-input", false, "allow all clients to write input")
	idleTimeout := flag.Duration("idle-timeout", 30*time.Minute, "idle timeout before session shutdown")
	tlsCert := flag.String("tls-cert", "", "path to TLS certificate (enables HTTPS)")
	tlsKey := flag.String("tls-key", "", "path to TLS private key (enables HTTPS)")
	logLevel := flag.String("log-level", "info", "log level: debug, info, warn, error")
	allowOrigin := flag.String("allow-origin", "", "allowed origins for WebSocket (comma-separated)")
	version := flag.Bool("version", false, "print version and exit")

	flag.Parse()

	if *version {
		fmt.Printf("vexshare %s\n", Version)
		os.Exit(0)
	}

	switch *authMode {
	case "password", "token", "password+token":
	default:
		fmt.Fprintf(os.Stderr, "Error: invalid auth mode %q. Use: password, token, password+token\n", *authMode)
		os.Exit(1)
	}

	var level slog.Level
	switch strings.ToLower(*logLevel) {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		fmt.Fprintf(os.Stderr, "Error: invalid log level %q. Use: debug, info, warn, error\n", *logLevel)
		os.Exit(1)
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	if *authMode == "password" || *authMode == "password+token" {
		if *password == "" {
			generated, err := tokens.GeneratePassword(18)
			if err != nil {
				logger.Error("failed to generate password", "error", err)
				os.Exit(1)
			}
			*password = generated
		}
	}

	if *authMode == "token" || *authMode == "password+token" {
		if *token == "" {
			generated, err := tokens.Generate()
			if err != nil {
				logger.Error("failed to generate token", "error", err)
				os.Exit(1)
			}
			*token = generated
		}
	}

	useTLS := *tlsCert != "" && *tlsKey != ""
	scheme := "http"
	if useTLS {
		scheme = "https"
	}

	authCfg := auth.Config{
		Mode:     *authMode,
		Username: *user,
		Password: *password,
		Token:    *token,
		Secure:   useTLS,
	}

	sessCfg := session.Config{
		Command:     *cmd,
		SharedInput: *sharedInput,
		IdleTimeout: *idleTimeout,
	}

	srvCfg := server.Config{
		ListenAddr:  *listen,
		TLSCert:     *tlsCert,
		TLSKey:      *tlsKey,
		AuthConfig:  authCfg,
		SessionCfg:  sessCfg,
		AllowOrigin: *allowOrigin,
		Logger:      logger,
	}

	printBanner(scheme, *listen, *authMode, *user, *password, *token, *cmd, *idleTimeout, *sharedInput)

	srv := server.New(srvCfg)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		fmt.Fprintln(os.Stderr, "\nShutting down...")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			logger.Error("shutdown error", "error", err)
		}
	}()

	if err := srv.Start(); err != nil {
		if err.Error() != "http: Server closed" {
			logger.Error("server error", "error", err)
			os.Exit(1)
		}
	}

	fmt.Fprintln(os.Stderr, "Goodbye.")
}

func printBanner(scheme, listen, authMode, user, password, token, cmd string, idleTimeout time.Duration, sharedInput bool) {
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "  ┌─────────────────────────────────────────────┐")
	fmt.Fprintln(os.Stderr, "  │           vexShare — Terminal Sharing       │")
	fmt.Fprintln(os.Stderr, "  └─────────────────────────────────────────────┘")
	fmt.Fprintln(os.Stderr)

	baseURL := fmt.Sprintf("%s://%s", scheme, listen)

	fmt.Fprintf(os.Stderr, "  Auth Mode    : %s\n", authMode)

	if authMode == "password" || authMode == "password+token" {
		fmt.Fprintf(os.Stderr, "  Username     : %s\n", user)
		fmt.Fprintf(os.Stderr, "  Password     : %s\n", password)
	}

	fmt.Fprintf(os.Stderr, "  URL          : %s\n", baseURL)

	if authMode == "token" || authMode == "password+token" {
		fmt.Fprintf(os.Stderr, "  Token URL    : %s/t/%s/\n", baseURL, token)
	}

	fmt.Fprintf(os.Stderr, "  Command      : %s\n", cmd)
	fmt.Fprintf(os.Stderr, "  Idle Timeout : %s\n", idleTimeout)

	if sharedInput {
		fmt.Fprintln(os.Stderr, "  Input        : shared (all clients can type)")
	} else {
		fmt.Fprintln(os.Stderr, "  Input        : single-controller")
	}

	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "  Press Ctrl+C to stop.")
	fmt.Fprintln(os.Stderr)
}
