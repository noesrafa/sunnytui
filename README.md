# sunnytui

TUI multiplexor de [Claude Code](https://claude.com/claude-code). Una sola terminal, varias sesiones de `claude` en paralelo, status visible, switching rápido.

## Instalación

```bash
brew install noesrafa/tap/sunnytui
```

Eso jala el binario precompilado para tu Mac (Intel o Apple Silicon). También funciona en Linux.

### Alternativas

```bash
# con Go instalado
go install github.com/noesrafa/sunnytui/cmd/sunnytui@latest

# desde fuente
git clone https://github.com/noesrafa/sunnytui
cd sunnytui && make build
```

## Uso

```bash
sunnytui
```

Necesitas tener el CLI `claude` instalado y autenticado.

## Desarrollo

```bash
make build   # compila a bin/sunnytui
make run     # build + run
make test    # go test ./...
```

## Licencia

MIT — ver [LICENSE](LICENSE).
