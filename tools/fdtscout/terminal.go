package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/exec"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
)

// This is a REAL PTY (github.com/creack/pty), not a hand-rolled command-and-response shell like
// the Windows app's own "drop to shell" pane -- Ctrl+C/Ctrl+D/Ctrl+Z, job control, and interactive
// programs (less, vim, etc.) all work natively because the remote end is a genuine kernel pty,
// the same primitive a real SSH server gives a real terminal client. The only thing this doesn't
// do is a full client-side VT100 emulator -- the browser side renders raw bytes into a
// scrolling <pre> and strips ANSI CSI/OSC sequences for readability rather than interpreting
// cursor-positioning escapes, same deliberate simplification as the WPF app's own shell pane.

var upgrader = websocket.Upgrader{
	// This console already sits behind its own password auth (requireAuth runs before this
	// handler) and is meant to be reached over the LAN, not proxied through arbitrary origins --
	// checking Origin here would mostly just break normal same-origin browser use, so it's
	// intentionally left permissive at this layer.
	CheckOrigin:     func(r *http.Request) bool { return true },
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
}

type resizeMessage struct {
	Type string `json:"type"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}

func handleTerminalWS(w http.ResponseWriter, r *http.Request, username string) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("terminal ws upgrade failed for %s: %v", username, err)
		return
	}
	defer conn.Close()

	shell := "/bin/bash"
	if _, err := os.Stat(shell); err != nil {
		shell = "/bin/sh"
	}
	cmd := exec.Command(shell, "-i")
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	ptmx, err := pty.Start(cmd)
	if err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte("\r\nfailed to start shell: "+err.Error()+"\r\n"))
		return
	}
	defer func() {
		_ = ptmx.Close()
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()

	log.Printf("terminal session opened by %s (pid %d)", username, cmd.Process.Pid)

	// pty -> browser
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				if writeErr := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); writeErr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// browser -> pty (keystrokes as binary/text frames, resize as a JSON control frame)
	for {
		msgType, data, err := conn.ReadMessage()
		if err != nil {
			break
		}
		if msgType == websocket.TextMessage {
			var resize resizeMessage
			if json.Unmarshal(data, &resize) == nil && resize.Type == "resize" && resize.Cols > 0 && resize.Rows > 0 {
				_ = pty.Setsize(ptmx, &pty.Winsize{Cols: uint16(resize.Cols), Rows: uint16(resize.Rows)})
				continue
			}
		}
		if _, err := ptmx.Write(data); err != nil {
			break
		}
	}

	log.Printf("terminal session closed for %s (pid %d)", username, cmd.Process.Pid)
}
