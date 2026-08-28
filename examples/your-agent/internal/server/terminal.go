package server

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
)

var terminalUpgrader = websocket.Upgrader{
	ReadBufferSize: 4096, WriteBufferSize: 4096, CheckOrigin: sameOriginCheck,
}

type terminalResize struct {
	Cols uint16 `json:"cols"`
	Rows uint16 `json:"rows"`
}

func (s *Server) handleTerminalWS(writer http.ResponseWriter, request *http.Request) {
	connection, err := terminalUpgrader.Upgrade(writer, request, nil)
	if err != nil {
		s.logger.Printf("terminal websocket upgrade: %v", err)
		return
	}
	defer connection.Close()
	if !s.registerTerminal(connection) {
		return
	}
	defer s.unregisterTerminal(connection)

	command := terminalCommand(s.config.WorkDir)
	terminal, err := pty.Start(command)
	if err != nil {
		_ = connection.WriteMessage(websocket.TextMessage, []byte("failed to start terminal: "+err.Error()))
		return
	}
	defer terminateTerminalProcess(command)
	_ = setWinsize(terminal, 100, 30)

	var workers sync.WaitGroup
	var closeOnce sync.Once
	closeTerminal := func() { closeOnce.Do(func() { _ = terminal.Close() }) }

	workers.Add(1)
	go func() {
		defer workers.Done()
		defer connection.Close()
		buffer := make([]byte, 4096)
		for {
			count, readErr := terminal.Read(buffer)
			if readErr != nil {
				return
			}
			if count > 0 {
				if writeErr := connection.WriteMessage(websocket.BinaryMessage, buffer[:count]); writeErr != nil {
					return
				}
			}
		}
	}()

	workers.Add(1)
	go func() {
		defer workers.Done()
		defer closeTerminal()
		for {
			messageType, data, readErr := connection.ReadMessage()
			if readErr != nil {
				return
			}
			if messageType == websocket.TextMessage {
				var resize terminalResize
				if json.Unmarshal(data, &resize) == nil && resize.Cols > 0 && resize.Rows > 0 {
					_ = setWinsize(terminal, resize.Cols, resize.Rows)
					continue
				}
			}
			if messageType == websocket.TextMessage || messageType == websocket.BinaryMessage {
				if _, writeErr := terminal.Write(data); writeErr != nil {
					return
				}
			}
		}
	}()

	workers.Wait()
	closeTerminal()
}

func (s *Server) registerTerminal(connection io.Closer) bool {
	s.terminalsMu.Lock()
	defer s.terminalsMu.Unlock()
	if s.stopping {
		_ = connection.Close()
		return false
	}
	s.terminals[connection] = struct{}{}
	s.terminalsWG.Add(1)
	return true
}

func (s *Server) unregisterTerminal(connection io.Closer) {
	s.terminalsMu.Lock()
	if _, exists := s.terminals[connection]; exists {
		delete(s.terminals, connection)
		s.terminalsWG.Done()
	}
	s.terminalsMu.Unlock()
}

func (s *Server) closeTerminalConnections() {
	s.terminalsMu.Lock()
	s.stopping = true
	connections := make([]io.Closer, 0, len(s.terminals))
	for connection := range s.terminals {
		connections = append(connections, connection)
	}
	s.terminalsMu.Unlock()
	for _, connection := range connections {
		_ = connection.Close()
	}
}

func sameOriginCheck(request *http.Request) bool {
	origin := strings.TrimSpace(request.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	parsedOrigin, err := url.Parse(origin)
	if err != nil || (parsedOrigin.Scheme != "http" && parsedOrigin.Scheme != "https") || parsedOrigin.Host == "" || parsedOrigin.User != nil || parsedOrigin.Path != "" || parsedOrigin.RawQuery != "" || parsedOrigin.Fragment != "" {
		return false
	}
	if request.TLS != nil && parsedOrigin.Scheme != "https" {
		return false
	}
	requestURL, err := url.Parse("//" + request.Host)
	if err != nil || requestURL.Host == "" || requestURL.User != nil {
		return false
	}
	requestHost, originHost := canonicalHost(requestURL.Hostname()), canonicalHost(parsedOrigin.Hostname())
	if requestHost == "" || originHost == "" || effectiveOriginPort(requestURL.Port(), parsedOrigin.Scheme) != effectiveOriginPort(parsedOrigin.Port(), parsedOrigin.Scheme) {
		return false
	}
	return requestHost == originHost || isLoopback(requestHost) && isLoopback(originHost)
}

func canonicalHost(host string) string { return strings.TrimSuffix(strings.ToLower(host), ".") }

func effectiveOriginPort(port, scheme string) string {
	if port != "" {
		return port
	}
	if scheme == "https" {
		return "443"
	}
	return "80"
}

func isLoopback(host string) bool {
	if canonicalHost(host) == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
