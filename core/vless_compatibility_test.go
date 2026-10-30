package core

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	panel "github.com/wyx2685/v2node/api/v2board"
	"github.com/wyx2685/v2node/conf"
	"github.com/wyx2685/v2node/limiter"
	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/common/protocol/tls/cert"
	"github.com/xtls/xray-core/common/uuid"
	xcore "github.com/xtls/xray-core/core"
	coreConf "github.com/xtls/xray-core/infra/conf"
)

// Exercise the node's listener, dynamic user registration, limiter and outbound
// together. Every connection, including the REALITY target, stays on loopback.
func TestVLESSForwardingCompatibility(t *testing.T) {
	payload := strings.Repeat("vless-forwarding-check\n", 4096)
	target := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, payload)
	}))
	target.EnableHTTP2 = true
	target.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
	target.StartTLS()
	defer target.Close()
	_, targetPort, err := net.SplitHostPort(target.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name     string
		security int
		flow     string
	}{
		{"tcp", panel.None, ""},
		{"tls", panel.Tls, ""},
		{"reality", panel.Reality, ""},
		{"reality_vision", panel.Reality, "xtls-rprx-vision"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			port := listener.Addr().(*net.TCPAddr).Port
			_ = listener.Close()
			userID := uuid.New()
			user := panel.UserInfo{Id: 1, Uuid: userID.String()}
			// The core intentionally blocks private destinations by default.
			// Permit only this loopback fixture through the panel route API.
			localOutbound := `{"tag":"loopback-test-only","protocol":"freedom","settings":{"finalRules":[{"action":"allow","ip":["127.0.0.1/32"]}]}}`
			node := &panel.NodeInfo{
				Id: 1, Type: "vless", Security: tc.security, Tag: "compatibility-test",
				Common: &panel.CommonNode{
					ListenIP: "127.0.0.1", ServerPort: port, Network: "tcp", Flow: tc.flow,
					NetworkSettings: json.RawMessage(`{}`),
					Routes:          []panel.Route{{Action: "route_ip", Match: []string{"127.0.0.1/32"}, ActionValue: &localOutbound}},
				},
			}
			stream := map[string]any{"network": "tcp"}
			switch tc.security {
			case panel.Tls:
				certificate, _ := cert.MustGenerate(nil, cert.DNSNames("test.invalid"))
				pem, key := certificate.ToPEM()
				pair, err := tls.X509KeyPair(pem, key)
				if err != nil {
					t.Fatal(err)
				}
				pin := sha256.Sum256(pair.Certificate[0])
				dir := t.TempDir()
				certPath, keyPath := filepath.Join(dir, "test.pem"), filepath.Join(dir, "test.key")
				if err := os.WriteFile(certPath, pem, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(keyPath, key, 0600); err != nil {
					t.Fatal(err)
				}
				node.Common.CertInfo = &panel.CertInfo{CertMode: "file", CertFile: certPath, KeyFile: keyPath}
				stream["security"] = "tls"
				stream["tlsSettings"] = map[string]any{"serverName": "test.invalid", "pinnedPeerCertSha256": fmt.Sprintf("%x", pin)}
			case panel.Reality:
				key, err := ecdh.X25519().GenerateKey(rand.Reader)
				if err != nil {
					t.Fatal(err)
				}
				node.Common.TlsSettings = panel.TlsSettings{
					ServerName: "test.invalid", Dest: "127.0.0.1", ServerPort: targetPort,
					PrivateKey: base64.RawURLEncoding.EncodeToString(key.Bytes()), ShortId: "0123456789abcdef",
				}
				stream["security"] = "reality"
				stream["realitySettings"] = map[string]any{
					"serverName": "test.invalid", "fingerprint": "chrome",
					"publicKey": base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()), "shortId": "0123456789abcdef",
				}
			}
			limiter.Init()
			limiter.AddLimiter("vless", node.Tag, []panel.UserInfo{user}, map[int]int{})
			t.Cleanup(func() { limiter.DeleteLimiter(node.Tag) })
			config := conf.New()
			config.LogConfig.Level = "warning"
			server := New(config)
			if err := server.Start([]*panel.NodeInfo{node}); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = server.Close() })
			if err := server.AddNode(node.Tag, node); err != nil {
				t.Fatal(err)
			}
			if _, err := server.AddUsers(&AddUsersParams{Tag: node.Tag, Users: []panel.UserInfo{user}, NodeInfo: node}); err != nil {
				t.Fatal(err)
			}
			clientJSON, err := json.Marshal(map[string]any{
				"log": map[string]any{"loglevel": "warning"},
				"outbounds": []any{map[string]any{
					"protocol": "vless", "streamSettings": stream,
					"settings": map[string]any{"vnext": []any{map[string]any{
						"address": "127.0.0.1", "port": port,
						"users": []any{map[string]any{"id": user.Uuid, "encryption": "none", "flow": tc.flow}},
					}}},
				}},
			})
			if err != nil {
				t.Fatal(err)
			}
			var clientConf coreConf.Config
			if err := json.Unmarshal(clientJSON, &clientConf); err != nil {
				t.Fatal(err)
			}
			built, err := clientConf.Build()
			if err != nil {
				t.Fatal(err)
			}
			client, err := xcore.New(built)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = client.Close() })
			if err := client.Start(); err != nil {
				t.Fatal(err)
			}
			transport := &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // Self-signed loopback test server.
				DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
					host, port, err := net.SplitHostPort(address)
					if err != nil {
						return nil, err
					}
					p, err := strconv.Atoi(port)
					if err != nil {
						return nil, err
					}
					return xcore.Dial(ctx, client, xnet.TCPDestination(xnet.ParseAddress(host), xnet.Port(p)))
				},
			}
			t.Cleanup(transport.CloseIdleConnections)
			httpClient := &http.Client{Transport: transport, Timeout: 10 * time.Second}
			response, err := httpClient.Get(target.URL)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			body, err := io.ReadAll(io.LimitReader(response.Body, int64(len(payload)+1)))
			if err != nil || string(body) != payload || response.StatusCode != http.StatusOK {
				t.Fatalf("forwarded response: status=%d bytes=%d error=%v", response.StatusCode, len(body), err)
			}
		})
	}
}
