# Discord Go Live Bypass — Linux

Adaptação para **Fedora 45** do [DiscordGoLiveBypass](https://github.com/thomassolcia/DiscordGoLiveBypass), com WireGuard nativo do Linux e inicialização automática pelo systemd.

Encaminha os destinos necessários do Discord por uma VPN fora do Brasil. No modo padrão, acompanha os logs do cliente para incluir as conexões de transmissão de tela no túnel. O roteamento usa endereços IP e também pode afetar outros aplicativos que acessam os mesmos destinos.

O funcionamento foi confirmado pelo usuário no Fedora 45 com uma configuração WireGuard da Proton VPN gratuita. Outras distribuições Linux ainda não foram validadas; os comandos deste tutorial são para Fedora tradicional, como Workstation e KDE. Fedora Atomic/Kinoite/Silverblue requer outro fluxo de instalação de pacotes no host.

## 1. Instalar as dependências e compilar

Abra o terminal e execute como seu usuário habitual:

```bash
sudo dnf install git golang wireguard-tools iproute
git clone https://github.com/Leonardov31/discord-golive-bypass-linux.git
cd discord-golive-bypass-linux
./fedora/build.sh test
./fedora/build.sh
```

O executável será criado em `build/discord-golive-bypass`. A compilação requer Go 1.23 ou superior e não depende de componentes do Windows.

## 2. Gerar o arquivo na Proton VPN gratuita

Você precisa de uma conta Proton VPN e de uma configuração WireGuard pessoal. Não precisa instalar o aplicativo Proton para usar esta ferramenta.

1. Acesse [account.protonvpn.com](https://account.protonvpn.com) e entre na sua conta gratuita.
2. Abra **Downloads → WireGuard configuration** (Configuração WireGuard).
3. Dê um nome à configuração, como `discord-fedora`.
4. Em **Platform**, escolha **Linux**.
5. Mantenha as opções padrão. No plano gratuito, a opção VPN disponível é **VPN Accelerator**.
6. Escolha um servidor gratuito fora do Brasil, identificado como **FREE**, entre os disponíveis na conta.
7. Clique em **Create** e, quando estiver pronto, em **Download**.
8. Salve o arquivo baixado na pasta **Downloads** e **renomeie-o para `proton.conf`**.

Esse procedimento segue as [instruções oficiais da Proton](https://protonvpn.com/support/wireguard-configurations). O arquivo deve ser WireGuard, com extensão `.conf`; arquivos OpenVPN `.ovpn` não servem.

**O arquivo contém sua chave privada. Não o publique no GitHub nem o envie em issues.** Este repositório ignora arquivos `.conf` e `.key` para reduzir o risco de inclusão acidental.

Outra VPN que forneça uma configuração WireGuard de saída fora do Brasil também pode ser utilizada.

## 3. Ativar e instalar o serviço

Ainda dentro da pasta `discord-golive-bypass-linux`, confira se o arquivo foi salvo com o nome indicado:

```bash
ls -l "$HOME/Downloads/proton.conf"
```

Se o arquivo aparecer, valide e instale:

```bash
./build/discord-golive-bypass --check --config "$HOME/Downloads/proton.conf" --home "$HOME"
sudo ./fedora/install.sh "$HOME/Downloads/proton.conf" "$HOME"
```

Se sua pasta de downloads ou o nome do arquivo for diferente, ajuste o caminho nos comandos. `proton.conf` precisa ser o arquivo real que você baixou e renomeou. O erro **“não foi possível abrir a configuração”** significa que o programa não conseguiu ler o caminho informado.

O instalador pede a senha de administrador, copia a configuração para `/etc/discord-golive-bypass/wireguard.conf` com permissão `600`, instala o executável e habilita o serviço systemd. Não instala o Discord.

Depois da instalação:

1. Feche completamente o Discord, inclusive pela bandeja do sistema.
2. Abra o Discord pelo atalho habitual.
3. Entre novamente na chamada e teste a transmissão de tela.

**Não é necessário executar o comando a cada reinicialização.** O serviço inicia automaticamente com o Fedora. No dia a dia, basta abrir o Discord. Uma chamada já aberta pode precisar ser encerrada e iniciada novamente após ativar ou trocar o modo.

## 4. Conferir, desativar e reativar

Consultar o estado e os registros:

```bash
systemctl status discord-golive-bypass --no-pager
sudo journalctl -u discord-golive-bypass -n 80 --no-pager
```

Desativar agora e impedir o início automático:

```bash
sudo systemctl disable --now discord-golive-bypass
```

Reativar agora e nas próximas inicializações:

```bash
sudo systemctl enable --now discord-golive-bypass
```

Para uma pausa temporária, use `sudo systemctl stop discord-golive-bypass`; para retomar, use `sudo systemctl start discord-golive-bypass`. Esses comandos não alteram a configuração de início automático.

## 5. Usar a câmera ou encaminhar mais tráfego do Discord

O modo padrão é `transmissao`, voltado ao Go Live. Para incluir voz e câmera, edite:

```bash
sudoedit /etc/discord-golive-bypass/settings
```

Altere somente a linha do modo para:

```ini
MODE=completo
```

Depois execute:

```bash
sudo systemctl restart discord-golive-bypass
```

Feche e reabra o Discord e entre novamente na chamada. Para voltar, use `MODE=transmissao` e reinicie o serviço. O modo completo inclui os destinos conhecidos do Discord, pode aumentar a latência da voz e não isola tráfego por processo.

## 6. Atualizar o programa ou trocar de servidor

Para atualizar o código e reinstalar, reutilizando a configuração já protegida:

```bash
git pull --ff-only
./fedora/build.sh test
./fedora/build.sh
sudo ./fedora/install.sh /etc/discord-golive-bypass/wireguard.conf "$HOME"
```

O instalador usa `transmissao` por padrão. Se quiser manter `completo`, acrescente `completo` ao final do último comando.

Para trocar de servidor, gere e baixe uma nova configuração WireGuard, renomeie-a para `proton.conf` na pasta Downloads e repita o comando da etapa 3. Faça isso também se o arquivo expirar ou o servidor deixar de funcionar.

## Limitações e diagnóstico

- Não há interface gráfica nem login Proton integrado.
- O modo transmissão depende dos logs do Discord nativo ou do Flatpak estável. Para navegador ou caminhos de log diferentes, use `completo`.
- Os endereços e o formato dos logs do Discord podem mudar. Não há garantia de funcionamento para todas as versões, contas ou redes.
- Se a VPN cair, os destinos selecionados podem ficar sem conexão até ela retornar ou o serviço ser desligado. Não há fallback automático de saúde nesta versão.
- O campo `DNS` do arquivo não é aplicado: o resolvedor do sistema continua em uso. Os comandos embutidos em configurações WireGuard, como `PostUp`, são rejeitados.
- Não altere firewalld ou desative SELinux para seguir este tutorial. Outras VPNs e regras de roteamento podem interferir.

Veja [modos, caminhos de logs, diagnóstico, recuperação após falha e desinstalação](fedora/README.md).

## Desenvolvimento e testes

```bash
go test ./...
go vet ./...
./fedora/build.sh
```

Os testes cobrem o núcleo de roteamento original, interpretação dos eventos de mídia, configurações inválidas, proteção do endpoint, falha parcial de aplicação de rotas, IPv6 e rotação/truncamento de logs. Há também um workflow de CI para testar e compilar no Linux. A instalação systemd e uma transmissão real precisam ser verificadas no computador de destino.

## Créditos e licença

Derivado de [thomassolcia/DiscordGoLiveBypass](https://github.com/thomassolcia/DiscordGoLiveBypass), sob licença [GPL-3.0](LICENSE). O núcleo de rotas e de detecção de transmissões mantém o código do projeto original. Consulte [NOTICE.md](NOTICE.md) para o commit de origem e as modificações da edição Linux.

Este projeto é independente e não é afiliado ao Discord ou à Proton.
