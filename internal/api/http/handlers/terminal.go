package handlers

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"sync"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/websocket/v2"

	"github.com/udaypankhaniya/trangly/internal/infra/docker"
)

// TerminalHandler handles container listing and interactive terminal sessions.
type TerminalHandler struct {
	docker *docker.Client
}

// NewTerminalHandler creates a TerminalHandler.
func NewTerminalHandler(docker *docker.Client) *TerminalHandler {
	return &TerminalHandler{docker: docker}
}

// ListContainers returns running containers, optionally filtered by compose project slug.
func (h *TerminalHandler) ListContainers(c *fiber.Ctx) error {
	ctx, cancel := requestCtx(c)
	defer cancel()

	slug := c.Query("project")
	containers, err := h.docker.ListContainers(ctx, slug)
	if err != nil {
		slog.Error("terminal: list containers", "err", err)
		return respondError(c, fiber.StatusInternalServerError, "failed to list containers")
	}
	return respondJSON(c, fiber.StatusOK, containers)
}

// ExecTerminal upgrades to WebSocket and provides an interactive shell in a container.
// The WebSocket handler receives keystrokes and resize events from the browser,
// and streams container stdout/stderr back.
func (h *TerminalHandler) ExecTerminal(c *websocket.Conn) {
	containerID := c.Params("id")
	if containerID == "" {
		slog.Warn("terminal: missing container ID")
		return
	}

	// Use a background context for the long-lived exec session.
	// Cancel it when the WebSocket closes to clean up the Docker exec.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := []string{"/bin/sh"}

	execID, err := h.docker.ExecCreate(ctx, containerID, cmd)
	if err != nil {
		slog.Error("terminal: exec create", "container", containerID, "err", err)
		return
	}

	hijack, err := h.docker.ExecAttach(ctx, execID)
	if err != nil {
		slog.Error("terminal: exec attach", "container", containerID, "err", err)
		return
	}
	defer hijack.Close()

	var wg sync.WaitGroup
	done := make(chan struct{})

	// Docker → WebSocket: stream container output to the browser.
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			n, readErr := hijack.Reader.Read(buf)
			if n > 0 {
				if wErr := c.WriteMessage(websocket.BinaryMessage, buf[:n]); wErr != nil {
					return
				}
			}
			if readErr != nil {
				if readErr != io.EOF {
					slog.Debug("terminal: docker read ended", "err", readErr)
				}
				return
			}
		}
	}()

	// WebSocket → Docker: forward keystrokes and handle resize events.
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(done)
		for {
			mt, msg, readErr := c.ReadMessage()
			if readErr != nil {
				return
			}
			switch mt {
			case websocket.BinaryMessage, websocket.TextMessage:
				// Check for resize control message: {"type":"resize","cols":N,"rows":N}
				if len(msg) > 0 && msg[0] == '{' {
					var resize struct {
						Type string `json:"type"`
						Cols uint   `json:"cols"`
						Rows uint   `json:"rows"`
					}
					if json.Unmarshal(msg, &resize) == nil && resize.Type == "resize" {
						if resize.Cols > 0 && resize.Rows > 0 {
							_ = h.docker.ExecResize(ctx, execID, resize.Rows, resize.Cols)
						}
						continue
					}
				}
				if _, wErr := hijack.Conn.Write(msg); wErr != nil {
					return
				}
			}
		}
	}()

	// Wait for the WebSocket reader to exit (client disconnected or error).
	<-done
	// Signal EOF to the container shell.
	hijack.CloseWrite()
	// Wait for the Docker reader goroutine to finish.
	wg.Wait()
}
