# Modos, diagnóstico e remoção

[Voltar ao tutorial de ativação](../README.md)

## Modos

- `transmissao` (padrão): gateway/API e mídias de Go Live detectadas nos logs
  entram no túnel. A lógica do upstream tenta preservar voz e conteúdo na conexão
  habitual. Se voz e transmissão usarem o mesmo IP, a rota desse IP afeta ambas.
- `completo`: as redes e os domínios conhecidos do Discord entram no túnel,
  incluindo voz e câmera. Pode aumentar a latência. O nome segue o upstream:
  trata-se de seleção por destinos IP, não de isolamento de todos os processos
  do Discord. Outros aplicativos que acessam esses IPs também usam o túnel.

Para trocar o modo, edite como administrador o arquivo abaixo e reinicie:

```bash
sudoedit /etc/discord-golive-bypass/settings
# MODE=completo  ou  MODE=transmissao
sudo systemctl restart discord-golive-bypass
```

O modo transmissão procura `renderer_js.log` em:

- `~/.config/discord/logs`, `discordptb/logs` e `discordcanary/logs`;
- `~/.var/app/com.discordapp.Discord/config/discord/logs` (Flatpak estável).

Se o Discord usa `XDG_CONFIG_HOME` personalizado, outro ID Flatpak ou deixa de
produzir esses logs no formato esperado, use `completo`. O navegador também
precisa do modo completo. A leitura começa no fim do arquivo: reabra a transmissão
após iniciar o serviço. Logs são lidos localmente; linhas completas não são
copiadas para o journal. O journal recebe eventos e IPs dos servidores de mídia.

## Verificar e diagnosticar

```bash
systemctl status discord-golive-bypass --no-pager
sudo journalctl -u discord-golive-bypass -n 80 --no-pager
sudo wg show dglb0 latest-handshakes
ip -4 route get 162.159.128.1
ip -4 route show dev dglb0
ip -6 route show metric 42760
```

Depois de gerar tráfego, `latest-handshakes` deve mostrar um timestamp diferente
de zero e recente. O comando de rota deve mostrar `dev dglb0` para os destinos
incluídos. Isso verifica o caminho local, mas não confirma o país do servidor nem
o funcionamento do Go Live. Faça o teste de transmissão e de recepção na chamada.

O arquivo deve ter um único `[Peer]`, endereço IPv4 em `Address`, `Endpoint` e
`AllowedIPs = 0.0.0.0/0`. Se trouxer endereço IPv6, precisa também de `::/0` nos
AllowedIPs. Configurações apenas IPv6 e VPNs que não oferecem saída completa não
são aceitas. Não há login Proton integrado nem interface gráfica nesta versão.

O backend aceita dados WireGuard, **nunca executa hooks** (`PostUp`, `PreDown`
etc.), e rejeita opções extras. `DNS` é aceito mas não aplicado: o resolvedor do
sistema permanece em uso. `MTU` é respeitado; sem valor, usa 1380. Sem endereço
IPv6 no túnel, instala rotas `unreachable` para os destinos IPv6 selecionados,
para que eles não usem a conexão IPv6 local por fora do túnel.

O serviço atualiza a resolução dos domínios a cada cinco minutos. As faixas IP e
o formato do log são os do upstream no commit
`4aa859f570ee38428f36cda275afb3a4038cbc00`; alterações no Discord podem exigir
atualização. Uma rota não identifica aplicativos nem nomes HTTPS. Confira a tabela
se outra VPN estiver ativa: regras de policy routing de outros produtos podem ter
precedência. O programa não modifica firewalld, SELinux nem a rota padrão.

Se o servidor VPN cair, as rotas ficam instaladas e o tráfego selecionado pode
parar até a VPN responder ou o serviço ser desligado. Esta versão não implementa
o monitoramento de saúde/fallback automático do serviço Windows.

```bash
sudo systemctl stop discord-golive-bypass
sudo systemctl start discord-golive-bypass
```

Em encerramentos normais e erros tratados, o processo remove somente as rotas
que conseguiu criar e sua interface. Se houver `SIGKILL` ou queda abrupta do
processo, confira antes de reiniciar:

```bash
ip link show dglb0
ip -6 route show type unreachable metric 42760
```

Depois de confirmar que são sobras desta ferramenta e que ela está parada:

```bash
sudo ip link del dglb0  # se ainda existir; remove também suas rotas de interface
sudo ip -6 route flush type unreachable metric 42760
```

A métrica `42760` é reservada por esta adaptação para suas rotas. Não a compartilhe
com rotas manuais. O processo recusa assumir uma interface `dglb0` preexistente
para evitar apagar uma interface de outra ferramenta.

## Remover

```bash
sudo systemctl disable --now discord-golive-bypass
sudo rm -f /etc/systemd/system/discord-golive-bypass.service
sudo rm -f /usr/local/libexec/discord-golive-bypass
sudo rm -f /etc/discord-golive-bypass/wireguard.conf /etc/discord-golive-bypass/settings
sudo rmdir /etc/discord-golive-bypass
sudo systemctl daemon-reload
```
