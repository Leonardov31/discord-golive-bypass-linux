package main

import (
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testConf = `[Interface]
PrivateKey = AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=
Address = 10.2.0.2/32
DNS = 10.2.0.1
[Peer]
PublicKey = AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=
AllowedIPs = 0.0.0.0/0
Endpoint = 203.0.113.8:51820
`

func TestLinuxConfig(t *testing.T) {
	c, e := parseLinuxConfig(testConf)
	if e != nil {
		t.Fatal(e)
	}
	if c.ipv6 || len(c.addresses) != 1 || strings.Contains(c.wire, "DNS") || strings.Contains(c.wire, "Address") {
		t.Fatal("configuração incorreta")
	}
	for _, bad := range []string{
		testConf + "PostUp = touch /tmp/no\n",
		strings.Replace(testConf, "[Peer]", "PostUp = touch /tmp/no\n[Peer]", 1),
		strings.Replace(testConf, "0.0.0.0/0", "10.0.0.0/8", 1),
		strings.Replace(testConf, "203.0.113.8", "162.159.128.1", 1),
		strings.Replace(testConf, "10.2.0.2/32", "fd00::2/128", 1),
		strings.Replace(testConf, "203.0.113.8:51820", "203.0.113.8:0", 1),
		testConf + "PublicKey = AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=\n",
	} {
		if _, e := parseLinuxConfig(bad); e == nil {
			t.Error("aceitou configuração inválida")
		}
	}
	secret := "segredo-nao-deve-aparecer"
	_, e = parseLinuxConfig(strings.Replace(testConf, "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=", secret, 1))
	if e == nil || strings.Contains(e.Error(), secret) {
		t.Fatal("erro vazou chave")
	}
}
func TestRoutesPreserveOwnershipOnFailure(t *testing.T) {
	a := netip.MustParsePrefix("66.22.192.0/18")
	b := netip.MustParsePrefix("195.62.89.0/24")
	calls := 0
	m := routeManager{applied: map[netip.Prefix]bool{}, endpoint: netip.MustParseAddr("203.0.113.8"), run: func(_ string, args ...string) error {
		calls++
		if calls == 2 {
			return errors.New("falha")
		}
		return nil
	}}
	if e := m.sync(map[netip.Prefix]bool{a: true, b: true}); e == nil || len(m.applied) != 1 {
		t.Fatal("perdeu controle de rotas parcialmente aplicadas")
	}
	m.run = func(_ string, args ...string) error {
		if args[2] != "del" {
			t.Fatal(args)
		}
		return nil
	}
	if e := m.sync(map[netip.Prefix]bool{}); e != nil || len(m.applied) != 0 {
		t.Fatal("limpeza falhou")
	}
}
func TestRejectEndpointRouteBeforeMutation(t *testing.T) {
	m := routeManager{applied: map[netip.Prefix]bool{}, endpoint: netip.MustParseAddr("203.0.113.8"), run: func(string, ...string) error { t.Fatal("alterou rede"); return nil }}
	if m.sync(map[netip.Prefix]bool{netip.MustParsePrefix("203.0.113.8/32"): true}) == nil {
		t.Fatal("aceitou recursão")
	}
}
func TestIPv6WithoutAddressIsUnreachable(t *testing.T) {
	a := routeArgs("add", netip.MustParsePrefix("2a0e:5940::/29"), false)
	if strings.Join(a, " ") != "-6 route add unreachable 2a0e:5940::/29 metric 42760 proto static" {
		t.Fatal(a)
	}
}
func TestTailRotationTruncationAndPartial(t *testing.T) {
	path := filepath.Join(t.TempDir(), "renderer_js.log")
	write := func(s string) {
		t.Helper()
		if e := os.WriteFile(path, []byte(s), 0600); e != nil {
			t.Fatal(e)
		}
	}
	write("linha antiga\n")
	tail := &linuxTail{path: path}
	defer tail.close()
	var got []string
	line := func(s string) { got = append(got, s) }
	tail.poll(time.Now(), line)
	f, e := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if e != nil {
		t.Fatal(e)
	}
	f.WriteString("nova")
	f.Close()
	tail.poll(time.Now(), line)
	f, _ = os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	f.WriteString(" linha\n")
	f.Close()
	tail.poll(time.Now(), line)
	if strings.Join(got, "|") != "nova linha" {
		t.Fatal(got)
	}
	os.Rename(path, path+".old")
	write("rotacionou\n")
	tail.poll(time.Now(), line)
	write("fim\n")
	tail.poll(time.Now(), line)
	if strings.Join(got, "|") != "nova linha|rotacionou|fim" {
		t.Fatal(got)
	}
}
