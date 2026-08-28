package server

import (
	"crypto/tls"
	"net/http/httptest"
	"testing"
)

func TestSameOriginCheck(t *testing.T) {
	request := httptest.NewRequest("GET", "http://127.0.0.1:8080/api/terminal/ws", nil)
	request.Host = "127.0.0.1:8080"
	request.Header.Set("Origin", "http://localhost:8080")
	if !sameOriginCheck(request) {
		t.Fatal("loopback aliases with the same port should be accepted")
	}
	request.Header.Set("Origin", "https://attacker.example")
	if sameOriginCheck(request) {
		t.Fatal("cross-origin request should be rejected")
	}
	request.Header.Set("Origin", "http://127.0.0.1:8080")
	request.TLS = &tls.ConnectionState{}
	if sameOriginCheck(request) {
		t.Fatal("HTTPS request should reject an HTTP origin")
	}
}
