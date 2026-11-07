#!/usr/bin/env bash
set -euo pipefail
cd -- "$(dirname -- "$0")/.."
if (( EUID != 0 )); then echo "Use: sudo ./fedora/install.sh /caminho/vpn.conf /home/usuario [transmissao|completo]" >&2; exit 1; fi
conf=${1:?Informe o arquivo WireGuard}
discord_home=${2:?Informe a pasta pessoal do usuário do Discord}
mode=${3:-transmissao}
# Keep EnvironmentFile values simple and unambiguous.
[[ $discord_home =~ ^/[a-zA-Z0-9_./-]+$ && -d $discord_home ]] || { echo 'Pasta pessoal inválida ou com caracteres não suportados' >&2; exit 1; }
[[ $mode == transmissao || $mode == completo ]] || { echo 'Modo inválido' >&2; exit 1; }
[[ -x build/discord-golive-bypass ]] || { echo 'Execute ./fedora/build.sh primeiro' >&2; exit 1; }
command -v wg >/dev/null || { echo 'Instale: sudo dnf install wireguard-tools iproute' >&2; exit 1; }
./build/discord-golive-bypass --check --config "$conf" --home "$discord_home" --mode "$mode"
# Changing the config while a service runs would leave it using the old peer.
systemctl stop discord-golive-bypass.service 2>/dev/null || true
install -d -m 700 /etc/discord-golive-bypass
install -D -m 755 build/discord-golive-bypass /usr/local/libexec/discord-golive-bypass
if [[ $(readlink -f -- "$conf") != /etc/discord-golive-bypass/wireguard.conf ]]; then
 install -m 600 -- "$conf" /etc/discord-golive-bypass/wireguard.conf
else
 chmod 600 /etc/discord-golive-bypass/wireguard.conf
fi
printf 'DISCORD_HOME=%s\nMODE=%s\n' "$discord_home" "$mode" > /etc/discord-golive-bypass/settings
chmod 600 /etc/discord-golive-bypass/settings
install -m 644 fedora/discord-golive-bypass.service /etc/systemd/system/discord-golive-bypass.service
if command -v restorecon >/dev/null; then
 restorecon -RF /usr/local/libexec/discord-golive-bypass /etc/discord-golive-bypass /etc/systemd/system/discord-golive-bypass.service
fi
systemctl daemon-reload
systemctl enable --now discord-golive-bypass.service
systemctl --no-pager status discord-golive-bypass.service
