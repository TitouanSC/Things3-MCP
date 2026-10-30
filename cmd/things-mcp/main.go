// Command things-mcp serves Things 3 over the Model Context Protocol.
//
// Local mode (macOS, stdio) is configured through the environment:
//
//	THINGS_AUTH_TOKEN  Things URL scheme token, needed for updates (optional)
//	THINGS_DB_PATH     path of the Things database (optional, auto-detected)
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/TitouanSC/Things3-MCP/internal/backend/local"
	"github.com/TitouanSC/Things3-MCP/internal/mcpserver"
)

// version is set at build time with -ldflags "-X main.version=…".
var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Getenv, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "things-mcp:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, getenv func(string) string, stderr io.Writer) error {
	fs := flag.NewFlagSet("things-mcp", flag.ContinueOnError)
	fs.SetOutput(stderr)
	transport := fs.String("transport", "stdio", "MCP transport: stdio")
	backendName := fs.String("backend", "local", "data source: local (macOS app)")
	showVersion := fs.Bool("version", false, "print the version and exit")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *showVersion {
		fmt.Fprintln(stderr, "things-mcp", version)
		return nil
	}
	if *transport != "stdio" {
		return fmt.Errorf("transport %q is not available yet (only stdio)", *transport)
	}
	if *backendName != "local" {
		return fmt.Errorf("backend %q is not available yet (only local)", *backendName)
	}
	if runtime.GOOS != "darwin" {
		return errors.New("the local backend needs macOS and the Things 3 app")
	}

	// stdout carries the MCP protocol: logs go to stderr only.
	logger := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	dbPath := getenv("THINGS_DB_PATH")
	if dbPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		if dbPath, err = local.FindDatabase(home); err != nil {
			return err
		}
	}
	token := getenv("THINGS_AUTH_TOKEN")
	b, err := local.New(local.Config{DBPath: dbPath, AuthToken: token})
	if err != nil {
		return err
	}
	defer b.Close()
	if token == "" {
		logger.Warn("THINGS_AUTH_TOKEN is not set: update tools will be refused")
	}

	srv := mcpserver.New(b, mcpserver.Options{Version: version, Logger: logger})
	logger.Info("serving", "transport", "stdio", "backend", "local", "version", version)
	if err := srv.Run(ctx, &mcp.StdioTransport{}); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}
