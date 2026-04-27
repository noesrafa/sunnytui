# sunnytui

Go TUI multiplexor de Claude Code. Una sola terminal, varias sesiones de `claude` en paralelo, status visible, switching rápido.

## Layout

```
cmd/sunnytui/main.go             entrypoint + subcommands
internal/claude/                 wrapper de `claude` CLI (proceso, decoder, tipos de eventos)
  events.go                      tipos del stream-json
  decoder.go                     line-delimited JSON → chan Event
  process.go                     RunOnce (one-shot)
  stream.go                      Stream long-lived (Send/Events/Close)
internal/session/                session manager: N procesos vivos, state machine
  transcript.go                  tipos de Item (UserItem, AssistantTextItem, etc.) — UI-free
  session.go                     Session: stream + state + Items + HandleEvent
  manager.go                     Manager: slice de Sessions + Active + Next/Prev/Close
internal/tui/                    Bubble Tea model/view/update
  styles.go                      tema central (lipgloss.AdaptiveColor)
  keymap.go                      KeyMap + bindings
  messages.go                    tea.Msg types
  transcript.go                  RenderItem switch sobre session.Item
  sidebar.go                     lista de sesiones con badges de estado
  dialog.go                      Overlay (stack de Dialog) + result msgs
  dialog_newsession.go           modal "nueva sesión" con textinput
  model.go                       root Model + Init/Update/Run + waitForSession
  view.go                        layout: header / sidebar+main / status
testdata/                        capturas reales de stream-json para referencia
```

## Build / run

```bash
make build                                            # binario en bin/sunnytui
./bin/sunnytui chat                                   # TUI interactiva
./bin/sunnytui chat --cwd /ruta --model sonnet --effort high
./bin/sunnytui spike "di pong"                        # M1: un turno, eventos decodificados
./bin/sunnytui stream-test "p1" "p2"                  # multi-turn en una sesión, sin TUI
make test
make fmt vet
```

**Modelos válidos:** `opus`, `sonnet`, `haiku` (alias) o nombre completo (`claude-sonnet-4-6`).
**Niveles de effort:** `low`, `medium`, `high`, `xhigh`, `max`.

## Stream-json — lo que necesitas saber

`claude -p <prompt> --output-format stream-json --verbose` emite líneas JSON. Tipos vistos:

- `system` (subtype `init`) — primer evento; trae `session_id`, `cwd`, `model`, `tools`, `slash_commands`.
- `rate_limit_event` — info de rate limit; ignorable para UI normal.
- `assistant` — `message.content[]` con bloques `text` y `tool_use` (`name`, `input`).
- `user` — typically tool_results; `message.content[]` con bloques `tool_result` (`content`).
- `result` — último evento; `is_error`, `duration_ms`, `total_cost_usd`, `num_turns`, `stop_reason`.
- `parse_error` — sintético del decoder cuando una línea no es JSON válido.

Sample completo en `testdata/stream-sample.jsonl`.

**Multi-turn / interactivo:** para M2+ usar `--input-format stream-json` y mandar mensajes del user por stdin como `{"type":"user","message":{"role":"user","content":[{"type":"text","text":"..."}]}}`. Un `result` por turno; entre turnos el proceso queda esperando stdin. `--resume <session_id>` retoma una sesión persistida en `~/.claude/projects/...`.

## Milestones

- **M1 (DONE)** — `sunnytui spike <prompt>`: un turno, eventos decodificados a color.
- **M2 (DONE)** — `sunnytui chat`: TUI Bubble Tea, una sesión interactiva, streaming, header/transcript/textarea/status, tema adaptativo.
- **M3 (DONE)** — multi-sesión: `Manager` + sidebar con badges (●/◐/✗), modal de nueva sesión, keys: `ctrl+n`/`tab`/`shift+tab`/`ctrl+w`. Cada sesión su propio cwd y proceso `claude`.
- **M3.5 (DONE) Polish** — live status real-time, tool blocks merged (spinner mientras corre, ✓/✗ + result preview cuando termina), glamour markdown para texto del assistant (cached), sidebar 2-line con elapsed/cost/error, file logging con `charmbracelet/log` a `~/.sunnytui/sunnytui.log`, welcome screen, tick-driven re-render para animar spinners en transcript.
- **M3.6 (DONE) Layout flip + features** — sidebar a la derecha (estilo Crush), logo `sunnytui™` con hatching diagonal, modal de confirmación para `ctrl+c`, renombrar sesión con `ctrl+r`. Colores fijos (no AdaptiveColor) y mouse motion deshabilitado para no filtrar OSC 11 / SGR mouse al textarea.
- **M3.7 (DONE) Big logo + cancel + model/effort + file picker** — logo de bloques "SUNNY" estilo Crush. `Esc` cancela el turno actual (SIGINT al proceso claude). Selector de modelo (opus/sonnet/haiku) y effort (low/medium/high/xhigh/max) en el modal de nueva sesión. File picker (bubbles/filepicker) reemplaza al textinput en el modal. Mouse motion re-habilitado y `tea.MouseMsg` ruteado SOLO al viewport (no al textarea) — eso evita que la altscreen scrollee la UI completa al rolar la rueda.
- **M3.8 (DONE) Escape leak fix + ergonomics + permissions** — Filtro `looksLikeLeakedEscape` que dropea fragmentos CSI/OSC (`<digits;digits;digits[Mm]`, `]11;rgb:`, `[<...`) que bubbletea no consumió en algunos terminales. Logo pink→purple gradient (top half pink, bottom half purple, hatching del mismo color). Quit dialog con botones `[Yep!] / Nope` estilo Crush (←/→ para cambiar, enter, atajos y/n). Textarea: `shift+enter` o `ctrl+j` newline; `ctrl+←/→` saltar palabras; `ctrl+del` borrar palabra adelante; `ctrl+backspace` borrar palabra atrás. CPU optimization: el tick chain del spinner muere cuando todas las sesiones están idle, se re-arma en `Send`. `--dangerously-skip-permissions` siempre activo (claude no pide permisos por cada Bash).
- **M3.9 (DONE) Bindings rework + drafts + dynamic input** — Bindings: `esc` abre el quit dialog (era cancelar turno); `ctrl+c` 1 vez limpia el textarea, 2× dentro de 1.5s cancela el turno (SIGINT). Per-tab drafts: cada `Session` tiene un `Draft` field; al cambiar de sesión se guarda el textarea actual y se restaura el de la nueva. Textarea con altura **dinámica** (3-12 filas) que crece con el contenido — workaround del bug de scroll interno de `bubbles/textarea` v1 (el viewport interno no seguía al cursor en SetHeight fijo). Logo todo morado (un solo color). Quitado `$cost` de header/status/sidebar/transcript (queda solo `turns` y duración por turno). Modal nueva sesión: `enter` crea (antes `ctrl+s`), `→/l` desciende en filepicker (libera enter), `←/h/backspace` sube. Newline solo con `ctrl+j` o `alt+enter` (shift+enter requiere Kitty/Modify-Other-Keys configurado en el terminal — no llega como evento distinto en Terminal.app/iTerm2 default).
- **M4 (DONE) v2 stack + Crush patterns** — migrado a `charm.land/{bubbletea,bubbles,lipgloss,glamour,log}/v2`. DynamicHeight, MouseEventFilter, backslash-escape (`\+enter` newline), MouseWheelMsg routing, View() declarativa con AltScreen+MouseMode. Modal overlay real con `lipgloss.NewCanvas + NewCompositor + NewLayer` (cells fuera del modal mantienen el chat detrás). 3 dialogs (Quit/Rename/NewSession) con `HatchedTitle` (pink→purple gradient via `lipgloss.Blend1D`).
- **M4.1 (DONE) Usage widget + statusline integration** — `internal/usage` lee/escribe `~/.sunnytui/usage-snapshot.json`. Subcomando `sunnytui statusline` actúa como Claude Code statusline (lee stdin JSON, persiste payload con `rate_limits.{five_hour,seven_day}.used_percentage`). Sidebar muestra barras pink/orange/red según %. Fallback al `rate_limit_event` del stream-json si no hay snapshot. `sunnytui statusline-install` imprime el snippet para registrar.
- **M4.2 (DONE) Runs feature** — `internal/runs` (Run + Manager + LogBuffer 500 lines), persistencia en `~/.sunnytui/runs.json`. Lanzamiento via `sh -c <cmd>` con setpgid para matar grupo entero. Stop con SIGTERM + 2s grace + SIGKILL. Sidebar widget bajo "usage". `Ctrl+U` abre RunsDialog (list + start/stop/restart/delete/new/logs). RunEditDialog para nuevos runs (name/command/cwd). RunLogsDialog viewport tail-style con auto-follow.
- **M4.3 (DONE) `sunny` shortcut + transcript persistence + live branch + ctx widget** — `sunny` (sin args) lanza el chat (alias del binario; `make build` produce `bin/sunny`, `make install` linkea). `state.SavedSession` ahora persiste `Items` (transcript completo via `session.MarshalItems`/`UnmarshalItems` con tagged union JSON), `TotalCost` y `Turns`; al reabrir se restaura todo. Save también dispara post-turn (no sólo en shutdown). Tick de 3s refresca `Session.Branch` para reflejar `git checkout` externos. Usage widget muestra barra `ctx` con `context_window.used_percentage` además de 5h/7d. Subcomando `sunnytui statusline-wrap CMD...` que persiste el snapshot y reenvía stdin/stdout a otro statusline (coexiste con claude-hud).
- **M4** — resume al reabrir, costo por modelo, settings dialog (cambiar modelo en sesión existente), `?` toggle full help.
- **M5** — `go install` + Goreleaser + brew tap.

## Bugs conocidos / observaciones

- **(arreglado en M3)** Si el modelo termina un turno sin emitir texto, antes la UI mostraba un `✓` huérfano. Ahora `Session.HandleEvent` rastrea `turnHadOutput` y inserta un `EmptyResponseItem` ("(sin respuesta)") cuando el `result` llega sin contenido del assistant.
- **(arreglado en M3.6)** Caracteres raros (`<64;78;29M`, `]11;rgb:...`) se filtraban al textarea al hacer scroll. Causa: `tea.WithMouseCellMotion` emitía SGR mouse events que bubbletea v1.3.10 no parseaba bien, y `lipgloss.AdaptiveColor` lanzaba OSC 11 background queries cuya respuesta también se filtraba. Solución: deshabilitar mouse motion (perdemos scroll-wheel pero el wheel emite up/down arrows que el viewport sí maneja), y reemplazar AdaptiveColor por colores fijos hex.
- **(arreglado en M2.5)** Al escribir, el header desaparecía. El textarea/cursor a veces metía una línea extra. Solución: clamp explícito del output de `textarea.View()` a `textareaInnerH` filas en `renderInput`, más un `clampHeight` defensivo en `View()` que trunca la salida total a `m.height` líneas.
- El `system/init` se re-emite al inicio de cada turno en streaming. Ignoramos init después del primero (`if s.RemoteID == ""` en `Session.HandleEvent`).

## Logging

`~/.sunnytui/sunnytui.log` — todos los eventos relevantes (sesión creada/cerrada, eventos del stream-json clasificados por tipo, errores). Útil:

```bash
tail -f ~/.sunnytui/sunnytui.log
```

## Patrones tomados de Crush (referencia)

- **Root model** = struct con sub-models como campos (chat, sidebar, dialogs, status); `tea.Batch()` para combinar Cmds.
- **Modales** = `dialog.Overlay` que es un stack de `Dialog` interface; los dialogs consumen mensajes primero, se renderean al final encima de todo.
- **Estilos** = struct central `Styles` con sub-structs por componente, theme functions que la devuelven, pasado vía `Common` a los sub-models.
- **Streaming** = se acumulan deltas en items, viewport hace scroll, glamour para markdown solo cuando el turno termina (parcial markdown rompe).
- **Layout** = se computa una vez por `WindowSizeMsg` y se propaga vía `SetSize/SetWidth` a hijos.
- **Input** = `bubbles/textarea` multi-line con `DynamicHeight=true`, enter envía, ctrl+j newline.

## Reglas

- Cada sesión = su propio `cwd` (Rafael lo pidió explícito). El manager debe aislar working dirs.
- NO PTY; siempre stream-json. La UI nativa de Claude Code se pelea con cualquier multiplexor.
- Iteración rápida > ahorrar tokens. Probar contra `claude` real, sin mocks.
