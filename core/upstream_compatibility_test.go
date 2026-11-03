package core

import (
	"bytes"
	"encoding/base64"
	"net"
	"testing"

	panel "github.com/wyx2685/v2node/api/v2board"
	"github.com/wyx2685/v2node/conf"
	"github.com/xtls/xray-core/app/proxyman"
	"github.com/xtls/xray-core/transport/internet"
)

// Start real listeners so key parsing also covers the placeholder user that
// exists before the panel supplies its first user list.
func TestShadowsocks2022PlaceholderStartup(t *testing.T) {
	for _, tc := range []struct {
		cipher string
		length int
	}{
		{"2022-blake3-aes-128-gcm", 16},
		{"2022-blake3-aes-256-gcm", 32},
	} {
		t.Run(tc.cipher, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			port := listener.Addr().(*net.TCPAddr).Port
			_ = listener.Close()
			node := &panel.NodeInfo{Id: 1, Type: "shadowsocks", Tag: "ss2022-placeholder", Common: &panel.CommonNode{
				ListenIP: "127.0.0.1", ServerPort: port, Network: "tcp", Cipher: tc.cipher,
				ServerKey: base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, tc.length)),
			}}
			config := conf.New()
			config.LogConfig.Level = "warning"
			server := New(config)
			if err := server.Start([]*panel.NodeInfo{node}); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = server.Close() })
			if err := server.AddNode(node.Tag, node); err != nil {
				t.Fatalf("SS2022 node could not start before user synchronization: %v", err)
			}
		})
	}
}

func TestDefaultOutboundDomainStrategy(t *testing.T) {
	outbound, err := buildDefaultOutbound()
	if err != nil {
		t.Fatal(err)
	}
	settings, err := outbound.SenderSettings.GetInstance()
	if err != nil {
		t.Fatal(err)
	}
	sender := settings.(*proxyman.SenderConfig)
	if sender.StreamSettings == nil || sender.StreamSettings.SocketSettings == nil {
		t.Fatal("default outbound has no socket DNS strategy")
	}
	if strategy := sender.StreamSettings.SocketSettings.DomainStrategy; strategy != internet.DomainStrategy_USE_IP46 {
		t.Fatalf("default outbound DNS strategy = %v, want USE_IP46", strategy)
	}
}
