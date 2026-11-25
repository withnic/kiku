# Phase 1: プロジェクト骨格・設定管理・Layer 1 検証・テキスト衛生ルール

## 実行前提・行動規範（厳守）
- 本タスクは自律型コーディングエージェント（Codex CLI / Antigravity / Gemini CLI / `claude -p` ＋ `ccw`）により完全非対話で実行されている。ユーザーへの質問・確認・選択の提示は一切行わず、`AGENTS.md`（ハードリンク先の `CLAUDE.md` / `GEMINI.md` 含む）および `docs/` 配下の設計書に従って最も安全かつ仕様に忠実な判断を下し、実装を完了させること。
- ローカルファイル操作・ビルド・テストコマンドの範囲内に厳密にとどめ、`git push` や許可外のシステム変更は絶対に行わないこと。

## 参照ドキュメント
実装前に必ず以下のファイルをすべて読み込み、仕様を完全に把握すること。
- `AGENTS.md`
- `docs/01_project_overview.md`
- `docs/02_data_modeling.md`
- `docs/03_architecture.md`

## Phase 1 実装タスク
1. **プロジェクト骨格・ハードリンク構成の整備**:
   - `go.mod`（`module github.com/withnic/kiku`、Go 1.23+、`github.com/goccy/go-yaml` 依存）および `docs/03_architecture.md` で定義されたディレクトリ構成を作成する。
   - `cmd/kiku/main.go` にビルド可能な最小エントリーポイント（Phase 3 で本実装するスタブ）を配置し、`go build ./cmd/kiku` が通る状態にする。
   - リポジトリ直下に `AGENTS.md` を配置し、`CLAUDE.md` および `GEMINI.md` が未作成の場合は同一ファイルのハードリンクとして作成する（Windows / Linux / WSL2 対応）。
2. **アトミック FS・設定ローダー（`internal/fs`, `internal/config`）の実装**:
   - `internal/fs`: 同一ディレクトリ一時ファイル＋`os.Rename` によるアトミック書き込み、パーミッション維持、パス正規化、シンボリックリンク非追従の探索ヘルパー、CRLF/LF 変換（`line_endings` 無効時の元改行コード維持を含む）を実装する。
   - `.kiku.yaml` の `include`、グローバル `ignore`、`.kikuignore`（1行1パターン・空行/`#` コメント無視）、および各ルール配下の `rules.<rule>.ignore` をパースし、グロブ（`**` 対応）および `re:` 正規表現を扱う `PathMatcher` を実装する（不正な正規表現は終了コード `2`）。
   - `internal/config`: `--config` 明示指定読み込みおよび指定ディレクトリ（カレントまたは対象ファイル親ディレクトリ）から親へ遡る `.kiku.yaml`（および `kiku.yaml`）探索（キャッシュ付）、未知フィールド拒否（設定エラー終了コード `2`）、デフォルト値の部分上書きマージ、`docs/02_data_modeling.md` 1.1節の全バリデーションを実装する。
3. **パーサ基盤・Layer 1 検証・テキスト衛生ルール（`internal/parser`, `internal/rules/hygiene.go`）の実装**:
   - `internal/parser`: 不正 UTF-8 検知、BOM 検知、`goccy/go-yaml`（`parser.ParseComments`）パース、重複キー検知（`key-duplicates`）、複数行リテラル（`|` / `>`）および複数行クォート文字列の保護行範囲抽出、および `# kiku-disable` / `# kiku-disable-next-line` / `# kiku-enable` / `# kiku:ignore` / `# kiku:ignore-section` / `# kiku:ignore-paragraph` / `# kiku:disable` / `# kiku:enable`（`-- 理由` コメント・`snake_case` 正規化対応）を解析し、行末・単一行・ブロック全体（インデント範囲の AST ノード全体）・段落（次の空行まで）・範囲指定の4粒度で `IgnoreMap` を構築する。また制御コメント行は `line-length` から除外する。
   - `internal/rules/hygiene.go`: `no-tabs`（`check` 時はエラー、`fix` 時は `indentation.spaces` 分の半角スペース置換）、`encoding`（BOM除去）、`line-endings`、`line-length`（`Fixable: false`、コメント/URL除外オプション付）、`trailing-whitespace`、`final-newline`（空ファイル許容・末尾余剰空行正規化）、`blank-lines` を実装する。複数行リテラル（`|` / `>`）およびクォート複数行文字列の内部を破壊しないよう保護すること。
4. **単体テストの作成**:
   - `internal/fs`, `internal/config`, `internal/parser`, `internal/rules` の正常系・異常系（重複キー、不正UTF-8、タブ混入、BOM、未知設定キー、`.kikuignore`、複数行リテラル保護）の単体テストを作成する。

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
- 全検証がグリーンになったことを確認した上で、作成したファイル一覧・テスト結果・Phase 2 への引き継ぎ事項を簡潔に出力して終了すること。
