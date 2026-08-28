//go:build !windows

package server

import (
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestTerminalWebSocketExecutesInWorkspace(t *testing.T) {
	root := t.TempDir()
	service, err := New(Config{DataDir: filepath.Join(root, "data"), WorkDir: root})
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(service.Handler())
	defer httpServer.Close()
	defer service.Close()

	target, _ := url.Parse(httpServer.URL)
	target.Scheme = "ws"
	target.Path = "/api/terminal/ws"
	connection, _, err := websocket.DefaultDialer.Dial(target.String(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if err := connection.WriteMessage(websocket.BinaryMessage, []byte("printf '__YOUR_AGENT_TERMINAL__\\n'\n")); err != nil {
		t.Fatal(err)
	}
	_ = connection.SetReadDeadline(time.Now().Add(5 * time.Second))
	var output strings.Builder
	for !strings.Contains(output.String(), "__YOUR_AGENT_TERMINAL__") {
		_, data, err := connection.ReadMessage()
		if err != nil {
			t.Fatalf("read terminal output: %v; output=%q", err, output.String())
		}
		output.Write(data)
	}
}
