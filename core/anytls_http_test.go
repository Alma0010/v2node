package core

import (
	"os"
	"path/filepath"
	"testing"

	panel "github.com/wyx2685/v2node/api/v2board"
	"github.com/xtls/xray-core/app/proxyman"
	"github.com/xtls/xray-core/common/protocol/tls/cert"
	coreConf "github.com/xtls/xray-core/infra/conf"
	"github.com/xtls/xray-core/proxy/anytls"
	xtls "github.com/xtls/xray-core/transport/internet/tls"
)

func TestAnyTLSHTTPRejectionConfiguration(t *testing.T) {
	certificate, _ := cert.MustGenerate(nil, cert.DNSNames("test.invalid"))
	pem, key := certificate.ToPEM()
	directory := t.TempDir()
	certFile, keyFile := filepath.Join(directory, "test.pem"), filepath.Join(directory, "test.key")
	if err := os.WriteFile(certFile, pem, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, key, 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, network, certMode string
		security                int
		override                *bool
		enabled, invalid        bool
	}{
		{"default_tls", "tcp", "remote", panel.Tls, nil, true, false},
		{"disabled", "tcp", "remote", panel.Tls, boolPtr(false), false, false},
		{"explicit", "tcp", "file", panel.Tls, boolPtr(true), true, false},
		{"websocket_unchanged", "ws", "remote", panel.Tls, nil, false, false},
		{"plaintext_unchanged", "tcp", "none", panel.None, nil, false, false},
		{"explicit_plaintext_invalid", "tcp", "none", panel.None, boolPtr(true), false, true},
		{"explicit_websocket_invalid", "ws", "remote", panel.Tls, boolPtr(true), false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info := &panel.NodeInfo{Type: "anytls", Security: tc.security, Common: &panel.CommonNode{Network: tc.network, PaddingScheme: []string{"stop=8"}, AnyTLSRejectHTTP: tc.override, CertInfo: &panel.CertInfo{CertMode: tc.certMode, CertFile: certFile, KeyFile: keyFile}}}
			in := &coreConf.InboundDetourConfig{}
			err := buildAnyTLS(info, in)
			if tc.invalid {
				if err == nil {
					t.Fatal("unsupported mode accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			info.Common.ServerPort = 8443
			info.Common.ListenIP = "127.0.0.1"
			built, err := buildInbound(info, "test")
			if err != nil {
				t.Fatal(err)
			}
			message, err := built.ProxySettings.GetInstance()
			if err != nil {
				t.Fatal(err)
			}
			cfg := message.(*anytls.ServerConfig)
			if cfg.RejectHttp != tc.enabled || cfg.PaddingScheme != "stop=8" {
				t.Fatalf("incorrect AnyTLS settings: %v", cfg)
			}
			if tc.security == panel.Tls {
				receiver, err := built.ReceiverSettings.GetInstance()
				if err != nil {
					t.Fatal(err)
				}
				transport := receiver.(*proxyman.ReceiverConfig).StreamSettings
				security, err := transport.SecuritySettings[0].GetInstance()
				if err != nil {
					t.Fatal(err)
				}
				alpn := security.(*xtls.Config).NextProtocol
				if tc.enabled && (len(alpn) != 1 || alpn[0] != "http/1.1") {
					t.Fatalf("HTTP rejection advertised incompatible ALPN: %v", alpn)
				}
				if !tc.enabled && len(alpn) != 0 {
					t.Fatalf("other transports unexpectedly changed ALPN: %v", alpn)
				}
			}
		})
	}
}

func boolPtr(value bool) *bool { return &value }
