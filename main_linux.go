//go:build linux

package main

import (
	"context"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const iface = "dglb0"
const metric = "42760"

type linuxConfig struct {
	wire      string
	addresses []netip.Prefix
	endpoint  netip.Addr
	mtu       int
	ipv6      bool
}

// Only recognized WireGuard data is accepted. Never execute wg-quick hooks.
func parseLinuxConfig(raw string) (c linuxConfig, err error) {
	c.mtu = 1380
	if len(raw) > 65536 {
		return c, errors.New("configuração maior que 64 KiB")
	}
	section := ""
	values := map[string]string{}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(strings.SplitN(line, "#", 2)[0])
		if line == "" || strings.HasPrefix(line, ";") {
			continue
		}
		if line == "[Interface]" || line == "[Peer]" {
			section = line
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if !ok || section == "" {
			return c, errors.New("estrutura WireGuard inválida")
		}
		key := section + k
		if _, exists := values[key]; exists {
			return c, fmt.Errorf("campo duplicado: %s", k)
		}
		allowed := (section == "[Interface]" && (k == "PrivateKey" || k == "Address" || k == "DNS" || k == "MTU")) || (section == "[Peer]" && (k == "PublicKey" || k == "PresharedKey" || k == "Endpoint" || k == "AllowedIPs" || k == "PersistentKeepalive"))
		if !allowed {
			return c, errors.New("campo não suportado; remova hooks, Table, SaveConfig e opções extras")
		}
		values[key] = v
	}
	for _, key := range []string{"[Interface]PrivateKey", "[Peer]PublicKey", "[Peer]PresharedKey"} {
		value := values[key]
		if value == "" && key == "[Peer]PresharedKey" {
			continue
		}
		decoded, e := base64.StdEncoding.DecodeString(value)
		if e != nil || len(decoded) != 32 {
			return c, errors.New("chave WireGuard ausente ou inválida")
		}
	}
	for _, rawAddr := range strings.Split(values["[Interface]Address"], ",") {
		addr, e := netip.ParsePrefix(strings.TrimSpace(rawAddr))
		if e != nil {
			return c, errors.New("Address inválido")
		}
		c.addresses = append(c.addresses, addr)
		c.ipv6 = c.ipv6 || addr.Addr().Is6()
	}
	has4 := false
	for _, a := range c.addresses {
		has4 = has4 || a.Addr().Is4()
	}
	if !has4 {
		return c, errors.New("Address precisa conter IPv4")
	}
	if m := values["[Interface]MTU"]; m != "" {
		n, e := strconv.Atoi(m)
		if e != nil || n < 1280 || n > 9000 {
			return c, errors.New("MTU inválido")
		}
		c.mtu = n
	}
	// Resolve endpoint before installing any routes and pin its address.
	host, port, e := net.SplitHostPort(values["[Peer]Endpoint"])
	if e != nil {
		return c, errors.New("Endpoint inválido")
	}
	p, e := strconv.Atoi(port)
	if e != nil || p < 1 || p > 65535 {
		return c, errors.New("porta do Endpoint inválida")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	addrs, e := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if e != nil || len(addrs) == 0 {
		return c, errors.New("não foi possível resolver Endpoint")
	}
	c.endpoint = addrs[0].Unmap()
	if covered(discordPrefixes, c.endpoint) {
		return c, errors.New("Endpoint dentro das redes do Discord")
	}
	allowed4, allowed6 := false, false
	for _, item := range strings.Split(values["[Peer]AllowedIPs"], ",") {
		a, e := netip.ParsePrefix(strings.TrimSpace(item))
		if e != nil {
			return c, errors.New("AllowedIPs inválido")
		}
		allowed4 = allowed4 || a.String() == "0.0.0.0/0"
		allowed6 = allowed6 || a.String() == "::/0"
	}
	if !allowed4 || (c.ipv6 && !allowed6) {
		return c, errors.New("use um peer de saída com AllowedIPs = 0.0.0.0/0 (e ::/0 se houver Address IPv6)")
	}
	keep := values["[Peer]PersistentKeepalive"]
	if keep == "" {
		keep = "25"
	}
	n, e := strconv.Atoi(keep)
	if e != nil || n < 0 || n > 65535 {
		return c, errors.New("PersistentKeepalive inválido")
	}
	c.wire = "[Interface]\nPrivateKey = " + values["[Interface]PrivateKey"] + "\n[Peer]\nPublicKey = " + values["[Peer]PublicKey"] + "\nAllowedIPs = 0.0.0.0/0, ::/0\nEndpoint = " + net.JoinHostPort(c.endpoint.String(), port) + "\nPersistentKeepalive = " + keep + "\n"
	if key := values["[Peer]PresharedKey"]; key != "" {
		c.wire += "PresharedKey = " + key + "\n"
	}
	return c, nil
}

func command(name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// Do not forward stderr: wg may quote private configuration data on errors.
	if err := exec.CommandContext(ctx, name, args...).Run(); err != nil {
		return fmt.Errorf("%s falhou (%v)", name, err)
	}
	return nil
}

func routeArgs(verb string, p netip.Prefix, ipv6 bool) []string {
	family := "-4"
	if p.Addr().Is6() {
		family = "-6"
	}
	args := []string{family, "route", verb}
	if p.Addr().Is6() && !ipv6 {
		args = append(args, "unreachable", p.String())
	} else {
		args = append(args, p.String(), "dev", iface)
	}
	return append(args, "metric", metric, "proto", "static")
}

type routeManager struct {
	applied  map[netip.Prefix]bool
	ipv6     bool
	endpoint netip.Addr
	run      func(string, ...string) error
}

func (m *routeManager) sync(want map[netip.Prefix]bool) error {
	for p := range want {
		if p.Contains(m.endpoint) {
			return errors.New("rota interceptaria o servidor da VPN")
		}
	}
	add, del := diffRoutes(m.applied, want)
	for _, p := range add {
		if err := m.run("ip", routeArgs("add", p, m.ipv6)...); err != nil {
			return fmt.Errorf("adicionar rota %s: %w", p, err)
		}
		m.applied[p] = true
	}
	for _, p := range del {
		if err := m.run("ip", routeArgs("del", p, m.ipv6)...); err != nil {
			return fmt.Errorf("remover rota %s: %w", p, err)
		}
		delete(m.applied, p)
	}
	return nil
}

// Follow replacement and truncation, retaining partial lines. First open starts at EOF.
type linuxTail struct {
	path   string
	file   *os.File
	info   os.FileInfo
	opened bool
	buf    lineBuffer
}

func (t *linuxTail) close() {
	if t.file != nil {
		t.file.Close()
		t.file = nil
	}
}
func (t *linuxTail) poll(_ time.Time, line func(string)) {
	info, err := os.Stat(t.path)
	if err != nil {
		return
	}
	if t.file != nil && !os.SameFile(t.info, info) {
		t.close()
		t.buf.reset()
	}
	if t.file == nil {
		f, e := os.Open(t.path)
		if e != nil {
			return
		}
		t.file = f
		t.info = info
		if !t.opened {
			f.Seek(0, io.SeekEnd)
		}
		t.opened = true
	}
	pos, err := t.file.Seek(0, io.SeekCurrent)
	if err != nil {
		return
	}
	if info.Size() < pos {
		t.file.Seek(0, io.SeekStart)
		t.buf.reset()
	}
	b := make([]byte, 32768)
	// Bound work even if another process continuously writes to the log.
	for i := 0; i < 32; i++ {
		n, e := t.file.Read(b)
		if n > 0 {
			t.buf.feed(b[:n], line)
		}
		if e != nil || n == 0 {
			break
		}
	}
}
func linuxLogPaths(home string) []string {
	var paths []string
	for _, name := range []string{"discord", "discordptb", "discordcanary"} {
		paths = append(paths, filepath.Join(home, ".config", name, "logs", "renderer_js.log"))
	}
	paths = append(paths, filepath.Join(home, ".var/app/com.discordapp.Discord/config/discord/logs/renderer_js.log"))
	return paths
}

func runLinux() error {
	config := flag.String("config", "/etc/discord-golive-bypass/wireguard.conf", "arquivo WireGuard com um peer fora do Brasil")
	home := flag.String("home", "", "pasta pessoal do usuário do Discord (obrigatória no modo transmissão)")
	mode := flag.String("mode", modeStream, "transmissao ou completo")
	check := flag.Bool("check", false, "validar configuração sem alterar rede")
	flag.Parse()
	if !validMode(*mode) {
		return errors.New("modo inválido: use transmissao ou completo")
	}
	if *mode == modeStream && (*home == "" || !filepath.IsAbs(*home)) {
		return errors.New("informe --home com caminho absoluto do usuário do Discord")
	}
	f, err := os.Open(*config)
	if err != nil {
		return errors.New("não foi possível abrir a configuração")
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, 65537))
	if err != nil {
		return errors.New("não foi possível ler a configuração")
	}
	cfg, err := parseLinuxConfig(string(raw))
	if err != nil {
		return err
	}
	if *check {
		fmt.Println("Configuração válida; nenhum comando de rede executado.")
		return nil
	}
	if os.Geteuid() != 0 {
		return errors.New("execute com sudo ou pelo serviço systemd")
	}
	// Fixed PATH prevents executing user-controlled utilities while privileged.
	os.Setenv("PATH", "/usr/sbin:/usr/bin:/sbin:/bin")
	for _, tool := range []string{"ip", "wg"} {
		if _, err := exec.LookPath(tool); err != nil {
			return fmt.Errorf("instale a dependência %s", tool)
		}
	}
	lock, err := os.OpenFile("/run/discord-golive-bypass.lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return errors.New("outra instância já está em execução")
	}
	// Never take over an existing interface.
	if err := command("ip", "link", "show", "dev", iface); err == nil {
		return errors.New("interface dglb0 já existe; confira se é de uma execução anterior antes de removê-la")
	}
	if err := command("ip", "link", "add", "dev", iface, "type", "wireguard"); err != nil {
		return err
	}
	defer func() {
		if err := command("ip", "link", "del", "dev", iface); err != nil {
			log.Print(err)
		}
	}()
	tmp, err := os.CreateTemp("/run", "dglb-wg-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(cfg.wire); err != nil {
		tmp.Close()
		return err
	}
	tmp.Close()
	if err := command("wg", "setconf", iface, tmp.Name()); err != nil {
		return err
	}
	os.Remove(tmp.Name())
	for _, a := range cfg.addresses {
		if err := command("ip", "address", "add", a.String(), "dev", iface); err != nil {
			return err
		}
	}
	if err := command("ip", "link", "set", "dev", iface, "mtu", strconv.Itoa(cfg.mtu), "up"); err != nil {
		return err
	}
	m := routeManager{map[netip.Prefix]bool{}, cfg.ipv6, cfg.endpoint, command}
	defer func() {
		if err := m.sync(map[netip.Prefix]bool{}); err != nil {
			log.Printf("limpeza de rotas: %v", err)
		}
	}()
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	resolved := ResolveDiscordHosts(5 * time.Second)
	var sources []lineSource
	if *mode == modeStream {
		for _, p := range linuxLogPaths(*home) {
			sources = append(sources, &linuxTail{path: p})
			log.Printf("acompanhando %s", p)
		}
	}
	watch := newMediaWatch(sources)
	defer func() {
		for _, s := range sources {
			s.close()
		}
	}()
	// A single event loop guarantees files are closed only after their final read.
	tick := time.NewTicker(logPollInterval)
	defer tick.Stop()
	refresh := time.NewTicker(5 * time.Minute)
	defer refresh.Stop()
	log.Printf("túnel ativo, modo %s; reinicie o Discord e reentre na chamada", *mode)
	for {
		want := map[netip.Prefix]bool{}
		for _, p := range routesFor(*mode, resolved) {
			want[p] = true
		}
		if *mode == modeStream {
			extra, _ := watch.state()
			for _, p := range extra {
				want[p] = true
			}
		}
		if err := m.sync(want); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case now := <-tick.C:
			if *mode == modeStream {
				watch.step(now)
			}
		case <-refresh.C:
			resolved = ResolveDiscordHosts(5 * time.Second)
		}
	}
}
func main() {
	if err := runLinux(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}
