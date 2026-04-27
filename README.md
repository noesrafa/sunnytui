# sunnytui

TUI multiplexor de [Claude Code](https://claude.com/claude-code). Una sola terminal, varias sesiones de `claude` en paralelo, status visible, switching rápido.

## Instalación

```bash
brew install noesrafa/tap/sunnytui
```

Jala un binario precompilado. Funciona en macOS (Intel + Apple Silicon) y Linux (amd64 + arm64). No requiere Go ni compilar nada.

Para actualizar:

```bash
brew upgrade sunnytui
```

Para desinstalar:

```bash
brew uninstall sunnytui
brew untap noesrafa/tap   # opcional
```

### Alternativas

```bash
# con Go instalado (compila en tu máquina)
go install github.com/noesrafa/sunnytui/cmd/sunnytui@latest

# desde fuente
git clone https://github.com/noesrafa/sunnytui
cd sunnytui && make build
```

## Requisitos

- El CLI [`claude`](https://claude.com/claude-code) instalado y autenticado.
- Una terminal moderna (Terminal.app, iTerm2, Ghostty, Kitty, Alacritty, WezTerm).

## Uso

```bash
sunnytui                                              # abre la TUI
sunnytui chat --cwd /ruta --model sonnet --effort high
sunnytui spike "di pong"                              # one-shot, eventos decodificados
```

**Modelos:** `opus`, `sonnet`, `haiku` (alias) o nombre completo (`claude-sonnet-4-6`).
**Effort:** `low`, `medium`, `high`, `xhigh`, `max`.

## Desarrollo

```bash
make build   # compila a bin/sunnytui
make run     # build + run
make test    # go test ./...
make fmt     # gofmt -w .
make vet     # go vet ./...
```

## Releases

Los releases se publican **automáticamente** cuando empujas un tag `vX.Y.Z`:

```bash
git tag v0.2.0
git push origin v0.2.0
```

Esto dispara el workflow [`.github/workflows/release.yml`](.github/workflows/release.yml), que corre [GoReleaser](https://goreleaser.com) en GitHub Actions y, en ~1 minuto:

1. Compila binarios para `darwin/amd64`, `darwin/arm64`, `linux/amd64`, `linux/arm64`.
2. Sube los `.tar.gz` + `checksums.txt` como assets del release de GitHub.
3. Regenera `Formula/sunnytui.rb` con los nuevos `sha256` y la pushea a [`noesrafa/homebrew-tap`](https://github.com/noesrafa/homebrew-tap).

A partir de ese momento, cualquier persona en cualquier Mac/Linux corre `brew upgrade sunnytui` (o `brew install noesrafa/tap/sunnytui` si es la primera vez) y obtiene la nueva versión.

### Configuración del pipeline

- **GoReleaser:** [`.goreleaser.yaml`](.goreleaser.yaml) — define los builds, archives y la fórmula de Homebrew.
- **Workflow:** [`.github/workflows/release.yml`](.github/workflows/release.yml) — corre en `push` de tags `v*`.
- **Secret:** `HOMEBREW_TAP_TOKEN` — un Personal Access Token con scope `repo`, configurado en *Settings → Secrets and variables → Actions* del repo `sunnytui`. Permite que GoReleaser haga `git push` a `homebrew-tap`.
- **Tap:** [`noesrafa/homebrew-tap`](https://github.com/noesrafa/homebrew-tap) — repo separado donde vive `Formula/sunnytui.rb`.

### Cómo cortar una nueva versión

```bash
# 1. asegúrate que main está limpio y verde
git status
go test ./...

# 2. tag siguiendo semver
git tag -a v0.2.0 -m "v0.2.0 — qué cambió"
git push origin v0.2.0

# 3. ver el progreso
gh run watch --repo noesrafa/sunnytui

# 4. cuando termina:
brew update && brew upgrade sunnytui
```

Si algo falla en el workflow, lo más común es que el `HOMEBREW_TAP_TOKEN` haya expirado — regenera el PAT y actualízalo:

```bash
gh secret set HOMEBREW_TAP_TOKEN --repo noesrafa/sunnytui
```

## Licencia

MIT — ver [LICENSE](LICENSE).
