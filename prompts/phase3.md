# Phase 3: 並列実行エンジン・CLI サブコマンド完成・E2E 結合テスト

## 実行前提・行動規範（厳守）
- 本タスクは自律型コーディングエージェント（Codex CLI / Antigravity / Gemini CLI / `claude -p` ＋ `ccw`）により完全非対話で実行されている。ユーザーへの質問・確認・選択の提示は一切行わず、`AGENTS.md`（ハードリンク先の `CLAUDE.md` / `GEMINI.md` 含む）および `docs/` 配下の設計書に従って最も安全かつ仕様に忠実な判断を下し、実装を完了させること。
- ローカルファイル操作・ビルド・テストコマンドの範囲内に厳密にとどめ、`git push` や許可外のシステム変更は絶対に行わないこと。

## 参照ドキュメント
実装前に必ず以下のファイルをすべて読み込み、仕様を完全に把握すること。
- `AGENTS.md`
- `docs/01_project_overview.md`
- `docs/02_data_modeling.md`
- `docs/03_architecture.md`

## Phase 2 からの引き継ぎ事項（既存実装の前提）
Phase 1・2 では以下が実装・検証済み（`go test -race ./...` グリーン、`goimports` / `gofmt -s` 導入済）。既存 API をそのまま活用し、重複実装を避けて Phase 3 を完成させること。
- **`internal/engine/pipeline.go`**: 単一ファイル処理関数 `Process(path, relativePath string, raw []byte, cfg *config.Resolved, fix bool) ([]byte, []parser.Diagnostic, error)` が実装済み（`parser.Parse` → `rules.Structure` → `rules.Hygiene` → `formatter.VerifyRoundTrip` を実行し、ファイル内診断を行・列・RuleID 昇順にソートして返す）。
  - **戻り値契約**: Layer 1 エラー（`invalid-utf8`, `no-tabs` check時, `syntax-error`, `key-duplicates`）時は `out == nil, err == nil` で `diagnostics` に `SeverityError` を返す（`fix` 時は元ファイルを絶対に上書きせず終了コード `1`）。`VerifyRoundTrip` 失敗や内部スパンエラー時は `out == nil, err != nil` を返す（元ファイルを上書きせず終了コード `2`）。
  - Phase 3 の `internal/engine/runner.go` では、この `Process` を `runtime.NumCPU()` 上限のワーカープールで並列実行し、全ファイルの診断結果を「ファイルパス昇順 → 行番号昇順 → 列番号昇順 → RuleID 昇順」に決定論的ソートすること。
- **`internal/fs/atomic.go`**: `WalkYAML(root, ignored, visit)`（シンボリックリンク非追従・ルートディレクトリ保護・ディレクトリ走査時の `.yaml`/`.yml` フィルタ済）、`AtomicWrite(path, data)`（既存ファイルのパーミッション維持＋一時ファイルアトミック置換）、`NormalizePath`、`RelativePath` を提供。なお `AtomicWrite` は既存ファイル前提（`os.Lstat`）のため、`kiku init` の新規 `.kiku.yaml` 生成には新規作成対応（または専用処理）を用いること。
- **`internal/config/config.go`**: `NewLoader(cwd, explicit, cliIgnore)` と `(*Loader).Load(dir)`（キャッシュ付親ディレクトリ遡及探索、`--config`、`.kikuignore`、`--ignore` マージ、全バリデーション）および `ErrorExitCode = 2` を提供。対象ファイルごとに `cfg.RelativePath(absPath)` で相対パスを解決し、グローバル `cfg.Matcher.Match(rel)` および `Process` 呼び出しへ渡すこと。
- **マッピングキー保護・テストフィクスチャ**: `truthy` はマッピングのキー（例: `on:`）を対象外とし、`sort-keys` は複数行リテラル値を含むマッピングも安全にソートできる状態になっている。また、`testdata/golden/`（`mixed`, `sort`, `workflow`, `hybrid`）、`testdata/valid/`（`complex.yaml`, `directives.yaml`）、`testdata/invalid/`（`duplicate_keys.yaml`, `syntax_error.yaml`, `tabs.yaml`）に実戦フィクスチャが整備済みなので、Phase 3 の E2E テストでも活用できる。

## Phase 3 実装タスク
1. **並列実行エンジン（`internal/engine/runner.go`）の実装**:
   - シンボリックリンク非追従のディレクトリ再帰探索（ディレクトリ走査時は `.yaml` / `.yml` 大文字小文字不問、直接指定ファイルは拡張子不問）、グローバル `ignore`（`.kiku.yaml` / `.kikuignore` / `--ignore` の統合、`filepath.ToSlash` 統一）による除外判定、ワーカープールによる並列ファイル処理、および決定論的ソート（パス → 行 → 列 → RuleID）による診断結果集約を実装する。
   - ディレクトリ走査時にグローバル `ignore` に一致するディレクトリは `filepath.SkipDir` でスキップし、各ルール適用前に `rules.<rule>.ignore` を評価してルール単位の除外を行う。
2. **CLI サブコマンド・レポーター完成（`internal/cli/`, `cmd/kiku/main.go`）**:
   - `kiku check [paths...]`: `--format`（`text` / `json` / `github`）、`--config`、`--ignore <pattern>`（複数指定可）フラグを実装し、違反時は終了コード `1`、設定・I/Oエラー時は終了コード `2` を返す。
   - CLI 引数 `[paths...]`（複数ディレクトリ・ファイル混在可）を優先し、省略時は `include` → `.` の順で対象を決定する。
   - `kiku fix [paths...]`: `--diff`（Unified Diff 出力・元ファイル非上書き）、`--stdout`（単一ファイル指定時の標準出力・元ファイル非上書き）、`--config`、`--ignore <pattern>`（複数指定可）フラグを実装する。`--stdout` に複数ファイル指定時や `--diff` 併用時は終了コード `2` とする。Layer 1 の修復不能エラー（構文エラー・重複キー等）を含むファイルは絶対に上書きせず、終了コード `1` を返す。
   - `kiku init`: カレントディレクトリにコメント付きの標準 `.kiku.yaml` を生成する（既存時は上書き防止エラー終了コード `2`、`--force` で上書き可）。
   - `kiku version`: バージョン情報を出力する。
3. **E2E 結合テスト・クロスプラットフォーム異常系テストの作成**:
   - 一時ディレクトリ上で実ファイルを対象とした `check` / `fix` / `init` の E2E テストを作成する。
   - E2E テストに、対象ディレクトリ引数指定、特定のファイル名除外、`.kikuignore`、正規表現 `re:` 除外、およびルール個別 `ignore` の動作検証を追加する。
   - 終了コード（`0` / `1` / `2`）の網羅検証、構文エラー・重複キー混在ファイルに対する元ファイル非破壊保証テスト、CRLF/LF 変換（および `line_endings` 無効時の元改行コード維持）テスト、`--diff` / `--stdout`（非上書き確認含む） / `--format json` の出力検証をすべて実装する。

## 完了条件（Exit Criteria / 3段階エスカレーション修復ループ）
- `gofmt -s -w . && go vet ./...`、`go test -v -race ./...`、`go build -o bin/kiku ./cmd/kiku`（Windows 環境では `bin/kiku.exe`）をすべて実行しグリーンにすること。
- エラーやテスト失敗が発生した場合は、同一箇所の無限ループを防ぐため、以下の**3段階エスカレーション修復ループ（各段階3回・計9回上限）**に従って粘り強く解決すること。
  1. **Level 1（試行 1〜3回目：実装レベルのデバッグ）**: エラー出力から該当関数・ロジック・型不整合を特定して修正する。3回で解決しない場合は同じ関数の小手先の修正を禁止し、Level 2 へ移行する。
  2. **Level 2（試行 4〜6回目：周辺環境・テスト前提の調査と構造変更）**: 中途半端なパッチをロールバック・整理した上で、テストコード・モックの前提ミス、OS・環境差異（Windows / WSL2 / macOS のパス・改行・プロセス挙動・権限）、並行処理やロック競合など別レイヤーを調査し、構造を変えて最大3回修正する。
  3. **Level 3（試行 7〜9回目：代替アプローチへのピボット）**: `docs/` の設計書に立ち返り、受け入れ基準を満たす別の技術アプローチやよりシンプルな代替設計へ方針転換して組み直し、最大3回検証する。
- **品質・コンテキスト維持ルール**:
  - **テストの改ざん禁止**: アサーションの削除・緩和やテストのスキップによって見かけ上パスさせる行為は厳禁とする。
  - **ログ出力の絞り込み**: テスト失敗時は全ログを垂れ流さず、失敗したテストケースとエラー出力に絞って確認し、コンテキスト肥大化を防ぐこと。
  - **最終セーフティネット（計9回・全3アプローチ完了後も未解決の場合）**: 試した3つのアプローチと結果、特定できた根本原因を `BLOCKED_REPORT.md` にまとめて終了すること。
- 全検証がグリーンになったことを確認した上で、作成したファイル一覧・テスト結果・全体の完了サマリーを簡潔に出力して終了すること。
