package core

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	panel "github.com/wyx2685/v2node/api/v2board"
	"github.com/wyx2685/v2node/conf"
	"github.com/wyx2685/v2node/limiter"
	"github.com/xtls/xray-core/common/uuid"
)

func mihomoTestPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	return port
}

// Run a real, separately isolated mihomo client. All traffic and listeners stay
// on loopback; the user's proxy configuration, controller and TUN are untouched.
func TestMihomoRealityCompatibility(t *testing.T) {
	binary := os.Getenv("V2NODE_MIHOMO_TEST_BINARY")
	if binary == "" {
		if os.Getenv("V2NODE_REQUIRE_MIHOMO_TEST") == "1" {
			t.Fatal("The pinned mihomo client is required in CI")
		}
		t.Skip("Set V2NODE_MIHOMO_TEST_BINARY to run the cross-client test")
	}
	for _, tc := range []struct {
		name, invalid string
		mlkem         bool
		fallback      bool
	}{
		{name: "legacy-default"},
		{name: "mlkem-enabled", mlkem: true},
		{name: "plain-tls-fallback", fallback: true},
		{name: "wrong-reality-key", invalid: "key"},
		{name: "wrong-vless-uuid", invalid: "uuid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var forwarded atomic.Int32
			var fallback atomic.Int32
			target := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/fallback-check" {
					fallback.Add(1)
					_, _ = io.WriteString(w, "website-fallback-ok")
					return
				}
				forwarded.Add(1)
				_, _ = io.WriteString(w, "mihomo-forwarding-ok")
			}))
			target.EnableHTTP2 = true
			target.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
			target.Config.ErrorLog = log.New(io.Discard, "", 0)
			target.StartTLS()
			t.Cleanup(target.Close)
			_, targetPort, _ := net.SplitHostPort(target.Listener.Addr().String())
			key, err := ecdh.X25519().GenerateKey(rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			id := uuid.New()
			user := panel.UserInfo{Id: 1, Uuid: id.String()}
			localOutbound := `{"tag":"test-loopback","protocol":"freedom","settings":{"finalRules":[{"action":"allow","ip":["127.0.0.1/32"]}]}}`
			node := &panel.NodeInfo{Id: 1, Type: "vless", Security: panel.Reality, Tag: "mihomo-fixture", Common: &panel.CommonNode{
				ListenIP: "127.0.0.1", ServerPort: mihomoTestPort(t), Network: "tcp", Flow: "xtls-rprx-vision", NetworkSettings: json.RawMessage(`{}`),
				TlsSettings: panel.TlsSettings{ServerName: "test.invalid", Dest: "127.0.0.1", ServerPort: targetPort, PrivateKey: base64.RawURLEncoding.EncodeToString(key.Bytes()), ShortId: "0123456789abcdef"},
				Routes:      []panel.Route{{Action: "default_out", ActionValue: &localOutbound}},
			}}
			limiter.Init()
			limiter.AddLimiter("vless", node.Tag, []panel.UserInfo{user}, map[int]int{})
			t.Cleanup(func() { limiter.DeleteLimiter(node.Tag) })
			configuration := conf.New()
			configuration.LogConfig.Level = "warning"
			if os.Getenv("V2NODE_COMPAT_DEBUG") == "1" {
				configuration.LogConfig.Level = "debug"
			}
			server := New(configuration)
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
			if tc.fallback {
				transport := &http.Transport{
					TLSClientConfig: &tls.Config{InsecureSkipVerify: true, ServerName: "test.invalid"}, // Self-signed fallback fixture.
					DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
						return (&net.Dialer{}).DialContext(ctx, network, net.JoinHostPort("127.0.0.1", strconv.Itoa(node.Common.ServerPort)))
					},
				}
				t.Cleanup(transport.CloseIdleConnections)
				client := &http.Client{Transport: transport, Timeout: 6 * time.Second}
				response, err := client.Get("https://test.invalid/fallback-check")
				if err != nil {
					t.Fatal(err)
				}
				defer response.Body.Close()
				body, err := io.ReadAll(io.LimitReader(response.Body, 1024))
				if err != nil || string(body) != "website-fallback-ok" || fallback.Load() != 1 || forwarded.Load() != 0 {
					t.Fatalf("fallback changed: body=%q fallback=%d forwarded=%d error=%v", body, fallback.Load(), forwarded.Load(), err)
				}
				return
			}
			publicKey := key.PublicKey().Bytes()
			if tc.invalid == "key" {
				other, err := ecdh.X25519().GenerateKey(rand.Reader)
				if err != nil {
					t.Fatal(err)
				}
				publicKey = other.PublicKey().Bytes()
			}
			clientID := user.Uuid
			if tc.invalid == "uuid" {
				other := uuid.New()
				clientID = other.String()
			}
			reality := map[string]any{"public-key": base64.RawURLEncoding.EncodeToString(publicKey), "short-id": "0123456789abcdef"}
			if tc.mlkem {
				reality["support-x25519mlkem768"] = true
			}
			port := mihomoTestPort(t)
			controllerAddress := net.JoinHostPort("127.0.0.1", strconv.Itoa(mihomoTestPort(t)))
			controllerSecret := "fixture-" + user.Uuid
			clientConfig := map[string]any{
				"mixed-port": port, "allow-lan": false, "bind-address": "127.0.0.1", "mode": "rule", "log-level": "warning", "ipv6": false,
				"external-controller": controllerAddress, "secret": controllerSecret, "tun": map[string]any{"enable": false}, "dns": map[string]any{"enable": false}, "ntp": map[string]any{"enable": false}, "geo-auto-update": false,
				"proxies": []any{map[string]any{"name": "fixture", "type": "vless", "server": "127.0.0.1", "port": node.Common.ServerPort, "uuid": clientID, "tls": true, "network": "tcp", "flow": "xtls-rprx-vision", "servername": "test.invalid", "client-fingerprint": "chrome", "reality-opts": reality}},
				"rules":   []string{"MATCH,fixture"},
			}
			if os.Getenv("V2NODE_COMPAT_DEBUG") == "1" {
				clientConfig["log-level"] = "debug"
			}
			data, err := json.Marshal(clientConfig)
			if err != nil {
				t.Fatal(err)
			}
			directory := t.TempDir()
			path := filepath.Join(directory, "config.yaml")
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			var logs bytes.Buffer
			command := exec.Command(binary, "-d", directory, "-f", path)
			configureMihomoTestProcess(command)
			command.Stdout, command.Stderr = &logs, &logs
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = command.Process.Kill(); _ = command.Wait() })
			proxyAddress := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
			controllerTransport := &http.Transport{Proxy: nil}
			t.Cleanup(controllerTransport.CloseIdleConnections)
			controllerClient := &http.Client{Transport: controllerTransport, Timeout: 200 * time.Millisecond}
			ready := false
			for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
				request, _ := http.NewRequest(http.MethodGet, "http://"+controllerAddress+"/version", nil)
				request.Header.Set("Authorization", "Bearer "+controllerSecret)
				response, err := controllerClient.Do(request)
				if err == nil {
					_ = response.Body.Close()
					ready = response.StatusCode == http.StatusOK
				}
				if ready {
					break
				}
				time.Sleep(25 * time.Millisecond)
			}
			if !ready {
				_ = command.Process.Kill()
				_ = command.Wait()
				t.Fatalf("mihomo did not start: %s", logs.String())
			}
			// The controller and listeners start before ApplyConfig marks the
			// tunnel Running. Applying this same isolated config synchronously
			// waits on the executor mutex and gives us a real readiness barrier.
			// Do not retry the actual forwarding request: it must work once ready.
			controllerClient.Timeout = 5 * time.Second
			reloadBody, _ := json.Marshal(map[string]string{"payload": string(data)})
			reload, _ := http.NewRequest(http.MethodPut, "http://"+controllerAddress+"/configs", bytes.NewReader(reloadBody))
			reload.Header.Set("Authorization", "Bearer "+controllerSecret)
			reload.Header.Set("Content-Type", "application/json")
			reloaded, err := controllerClient.Do(reload)
			if err != nil {
				t.Fatal(err)
			}
			_ = reloaded.Body.Close()
			if reloaded.StatusCode != http.StatusNoContent {
				t.Fatalf("mihomo fixture config was not applied: %s", reloaded.Status)
			}
			proxyURL, _ := url.Parse("http://" + proxyAddress)
			transport := &http.Transport{Proxy: http.ProxyURL(proxyURL), TLSClientConfig: &tls.Config{InsecureSkipVerify: true}} // Self-signed loopback fixture.
			t.Cleanup(transport.CloseIdleConnections)
			client := &http.Client{Transport: transport, Timeout: 6 * time.Second}
			response, requestErr := client.Get(target.URL)
			var body []byte
			if response != nil {
				body, err = io.ReadAll(io.LimitReader(response.Body, 1024))
				_ = response.Body.Close()
			}
			if tc.invalid != "" {
				if forwarded.Load() != 0 || string(body) == "mihomo-forwarding-ok" {
					t.Fatal("invalid credentials reached the destination")
				}
				return
			}
			if requestErr != nil || err != nil || string(body) != "mihomo-forwarding-ok" || forwarded.Load() != 1 {
				time.Sleep(250 * time.Millisecond)
				_ = command.Process.Kill()
				_ = command.Wait()
				t.Fatalf("mihomo forwarding failed: request=%v read=%v body=%q forwarded=%d client=%s", requestErr, err, body, forwarded.Load(), logs.String())
			}
			traffic, err := server.GetUserTrafficSlice(node.Tag, 0)
			if err != nil {
				t.Fatal(err)
			}
			var transferred int64
			for _, record := range traffic {
				transferred += record.Upload + record.Download
			}
			if transferred == 0 {
				t.Fatal("destination was reached without authenticated VLESS traffic")
			}
		})
	}
}
