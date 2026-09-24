# Wtyczka Synchro dla VS Code — plan

> **Status:** zaimplementowane w `vscode/` (wersja 0.1.0): punkty 1–5 z
> „Kolejności prac” i job CI (check, test, build). Publikacja w Marketplace
> jeszcze nie jest zrobiona. Jak uruchomić: `vscode/README.md`, sekcja Development.

Wtyczka to cienka warstwa TypeScript nad binarką `synchro`: uruchamia ją w tle,
czyta zdarzenia JSON z jej stdout i pokazuje je w UI VS Code. Cała logika
synchronizacji zostaje w Go.

## Gdzie trzymać kod

**W tym repo, w podkatalogu `vscode/`.**

```
synchro/
  cmd/synchro/
  internal/
  vscode/                 ← wtyczka
    package.json          ← manifest: komendy, ustawienia, activationEvents
    src/extension.ts
    tsconfig.json
    bin/                  ← binarki kopiowane przez Makefile (w .gitignore)
  Makefile                ← cel `vscode-bin` / `vscode-package`
```

Dlaczego razem:
- binarka i wtyczka zależą od wspólnego kontraktu (flagi, format zdarzeń
  `--json`). Zmiana po obu stronach idzie w jednym commicie i PR, więc wersje
  się nie rozjeżdżają,
- wtyczka pakuje binarkę zbudowaną z tego samego commita,
- `go build ./...` / `go test ./...` ignorują `vscode/` (nie ma tam plików `.go`),
  a Makefile już robi cross-compile.

Kiedy przenieść do osobnego repo: gdy wtyczka zacznie żyć własnym życiem
(inni maintainerzy, własny cykl wydań, obsługa kilku wersji binarki naraz).
Na start to niepotrzebny narzut.

Wersjonowanie w jednym repo: osobne tagi, np. `v1.4.0` dla CLI i
`vscode-v0.1.0` dla wtyczki, oraz osobny `vscode/CHANGELOG.md`
(Marketplace pokazuje go na stronie wtyczki).

## Kontrakt z binarką (już zaimplementowany)

| Potrzeba wtyczki                  | Jak to robi binarka                                                                |
| --------------------------------- | ---------------------------------------------------------------------------------- |
| Parsowalny output                 | `--json`: jedno zdarzenie JSON na linię, wszystko na stdout, bez logo i kolorów    |
| Hasło poza plikiem                | env `SYNCHRO_PASSWORD` ma pierwszeństwo przed `password` z `.synchro.json`         |
| Łagodne zatrzymanie (macOS/Linux) | SIGTERM → `stopping` → dokończenie trwających operacji → `stopped`, exit 0         |
| Łagodne zatrzymanie (Windows)     | `--stop-on-stdin-close`: zamknięcie stdin działa jak SIGTERM                       |
| Brak osieroconych procesów        | przy `--stop-on-stdin-close` śmierć VS Code zamyka pipe, a binarka sama się kończy |
| Zawieszone zatrzymanie            | drugi sygnał albo 15 s bez końca → natychmiastowe wyjście                          |
| Tworzenie configu                 | `synchro --init` w `cwd` = katalog workspace                                       |
| Test połączenia                   | `synchro --test`, exit 0/1 i zdarzenie `success`/`error`                           |

Zdarzenia `--json` (pełna tabela w README, sekcja „JSON output”):

```
start          {version}
connected      {username, host}
disconnected   {retryInSeconds}
syncAllStart   {total, workers}
syncAllDone    {uploaded, total}
watching       {local, remote, exclude}
change         {change: add|change|unlink|addDir|unlinkDir, path}
upload|delete|mkdir|rmdir  {path}
info|success|warn|error    {message}
stopping       {reason}
stopped
```

Każde zdarzenie ma też `time` i `level`.

## Funkcje wtyczki

### Komendy (paleta Ctrl/Cmd+Shift+P)

| Komenda                    | Działanie                                                                                                                                                                  |
| -------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `Synchro: Create config`   | `synchro --init` z `cwd` = root workspace, potem otwiera `.synchro.json` w edytorze. Alternatywa: formularz `showInputBox` (host, user, auth, katalogi) i zapis pliku w TS |
| `Synchro: Start sync`      | spawn `synchro --json --stop-on-stdin-close --sync`                                                                                                                        |
| `Synchro: Sync all`        | jak wyżej, ale `--syncAll`                                                                                                                                                 |
| `Synchro: Stop`            | `proc.stdin.end()` (działa wszędzie); fallback `proc.kill('SIGTERM')` po kilku sekundach, `proc.kill('SIGKILL')` na końcu                                                  |
| `Synchro: Test connection` | `synchro --json --test`, wynik jako `showInformationMessage` / `showErrorMessage`                                                                                          |
| `Synchro: Set password`    | `showInputBox({password: true})` i zapis w `context.secrets` (SecretStorage = keychain systemu)                                                                            |
| `Synchro: Show log`        | `outputChannel.show()`                                                                                                                                                     |

### UI

- **Output Channel „Synchro”**: czytelna wersja każdego zdarzenia JSON
  (np. `12:01:03 ↑ src/app.go`).
- **Pasek statusu**:
  - `$(sync~spin) Synchro` podczas łączenia i syncAll (z `uploaded/total`),
  - `$(check) Synchro` po `watching`,
  - `$(warning) Synchro` po `disconnected`,
  - `$(circle-slash) Synchro` gdy proces nie działa.

  Kliknięcie włącza albo wyłącza sync.
- **Powiadomienia**: tylko dla `error` i utraty połączenia, żeby nie spamować
  przy każdym uploadzie.
- Opcjonalnie później: progress (`window.withProgress`) dla syncAll
  na podstawie `syncAllStart` i liczby zdarzeń `upload`.

### Ustawienia (`contributes.configuration`)

- `synchro.binaryPath`: własna ścieżka do binarki (domyślnie dołączona).
- `synchro.configPath`: domyślnie `${workspaceFolder}/.synchro.json`.
- `synchro.autoStart`: czy startować sync po otwarciu workspace z configiem.
- `synchro.quiet`: przekazuje `--quiet`.

### Aktywacja

```json
"activationEvents": ["workspaceContains:.synchro.json"]
```

Komendy z `contributes.commands` aktywują wtyczkę same (VS Code ≥ 1.74).

## Szkielet kodu (TypeScript)

```ts
import * as vscode from 'vscode';
import { spawn, ChildProcess } from 'child_process';
import * as readline from 'readline';

let proc: ChildProcess | undefined;

async function start(ctx: vscode.ExtensionContext, mode: '--sync' | '--syncAll') {
  if (proc) return;
  const folder = vscode.workspace.workspaceFolders?.[0];
  if (!folder) return;

  const env = { ...process.env };
  const password = await ctx.secrets.get(`synchro:${folder.uri.fsPath}`);
  if (password) env.SYNCHRO_PASSWORD = password;

  proc = spawn(binaryPath(ctx), ['--json', '--stop-on-stdin-close', mode], {
    cwd: folder.uri.fsPath,
    env,
    stdio: ['pipe', 'pipe', 'pipe'],
  });

  readline.createInterface({ input: proc.stdout! }).on('line', (line) => {
    let event: any;
    try { event = JSON.parse(line); } catch { output.appendLine(line); return; }
    handleEvent(event);        // aktualizacja paska statusu, logu, powiadomień
  });
  proc.stderr!.on('data', (d) => output.append(d.toString()));
  proc.on('exit', (code) => { proc = undefined; setStatus('stopped', code); });
}

function stop() {
  if (!proc) return;
  const p = proc;
  p.stdin?.end();                                   // łagodnie, także na Windows
  const term = setTimeout(() => p.kill('SIGTERM'), 5_000);
  const kill = setTimeout(() => p.kill('SIGKILL'), 20_000);
  p.once('exit', () => { clearTimeout(term); clearTimeout(kill); });
}

export function deactivate() { stop(); }
```

`binaryPath` wybiera `context.asAbsolutePath('bin/synchro' + (process.platform === 'win32' ? '.exe' : ''))`,
chyba że użytkownik ustawił `synchro.binaryPath`.

Uwaga na `chmod`: VSIX może zgubić bit wykonywalności, więc przy aktywacji
na macOS/Linux warto zrobić `fs.chmodSync(path, 0o755)`.

## Dystrybucja binarki

**Rekomendacja: osobny VSIX per platforma** (`vsce package --target <platforma>`).
Marketplace sam podaje użytkownikowi właściwy plik.

| target VS Code | GOOS/GOARCH   |
| -------------- | ------------- |
| `darwin-arm64` | darwin/arm64  |
| `darwin-x64`   | darwin/amd64  |
| `linux-x64`    | linux/amd64   |
| `linux-arm64`  | linux/arm64   |
| `win32-x64`    | windows/amd64 |

Cel w Makefile (szkic):

```make
VSCODE_TARGETS := darwin-arm64:darwin/arm64 darwin-x64:darwin/amd64 \
                  linux-x64:linux/amd64 linux-arm64:linux/arm64 win32-x64:windows/amd64

vscode-package: ## Build per-platform VSIX packages.
	@for t in $(VSCODE_TARGETS); do \
		target=$${t%%:*}; os=$${t#*:}; os=$${os%/*}; arch=$${t##*/}; \
		ext=$$( [ $$os = windows ] && echo .exe ); \
		rm -rf vscode/bin && mkdir -p vscode/bin; \
		GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o vscode/bin/synchro$$ext $(CMD); \
		(cd vscode && npx vsce package --target $$target); \
	done
```

Alternatywa: jeden VSIX, a binarka pobierana z GitHub Releases przy pierwszym
uruchomieniu. Paczka jest mniejsza, ale trzeba obsłużyć pobieranie, weryfikację
sumy kontrolnej i brak sieci.

## CI (GitHub Actions)

- Obecny job Go bez zmian.
- Nowy job `vscode`: `npm ci`, `npm run lint`, `npm run compile` w `vscode/`.
- Job release na tagu `vscode-v*`: `make vscode-package`, potem
  `npx vsce publish --packagePath vscode/*.vsix` (token `VSCE_PAT` w sekretach).
  Opcjonalnie `npx ovsx publish` dla Open VSX (VSCodium, Cursor).

## Kolejność prac

1. `npx --package yo --package generator-code -- yo code` w `vscode/`
   (TypeScript, esbuild), dodać `vscode/node_modules`, `vscode/out`,
   `vscode/bin`, `*.vsix` do `.gitignore`.
2. Start / Stop / Output Channel / pasek statusu z binarką z `synchro.binaryPath`.
3. Create config, Test connection, Set password (SecretStorage → `SYNCHRO_PASSWORD`).
4. `autoStart`, powiadomienia o błędach, progress dla syncAll.
5. Makefile `vscode-package`, lokalny test przez „Install from VSIX…”.
6. CI i publikacja (konto publishera na marketplace.visualstudio.com).

Szacunek: MVP (punkty 1–3) około 1–2 dni, całość z paczkami i CI kilka dni.

## Pomysły na później

- Komenda „Upload current file” (wymaga trybu jednorazowego w binarce, np. `--upload <path>`).
- Obsługa multi-root workspace (osobny proces na folder).
- Weryfikacja host key (known_hosts), bo obecnie binarka akceptuje każdy klucz
  (patrz README, sekcja Security). Wtyczka mogłaby pytać o fingerprint przy
  pierwszym połączeniu.
