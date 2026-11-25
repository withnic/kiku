# **03\_architecture — 規矩（kiku）**

# **1\. システム構成とコンポーネント分割**

「規矩（kiku）」はクリーンな4層レイヤードアーキテクチャを採用し、YAML パーサライブラリ（`goccy/go-yaml`）および OS 固有のファイルシステム操作をドメインルールから明確に分離する。

```
┌──────────────────────────────────────────────────────────┐
│  cmd/kiku & internal/cli (CLI引数解析・出力フォーマッタ)    │
└────────────────────────────┬─────────────────────────────┘
                             ▼
┌──────────────────────────────────────────────────────────┐
│  internal/engine (2層パイプライン制御・並列ファイル実行)     │
└──────┬─────────────────────┬─────────────────────┬───────┘
       ▼                     ▼                     ▼
┌──────────────┐   ┌──────────────────┐   ┌────────────────┐
│internal/     │   │internal/rules    │   │internal/       │
│parser        │   │(Layer1 & Layer2  │   │formatter       │
│(goccy/go-yaml│   │ Lint/Fixルール群) │   │(CST/トークン    │
│ AST+コメント) │   │                  │   │ 差分再構築・検証)│
└──────────────┘   └──────────────────┘   └────────────────┘
       │                                           │
       └─────────────────────┬─────────────────────┘
                             ▼
┌──────────────────────────────────────────────────────────┐
│  internal/fs (クロスプラットフォームFS・アトミック書込)     │
└──────────────────────────────────────────────────────────┘
```

## **各パッケージの責務**

* **`internal/cli`**: `check` / `fix` / `init` / `version` サブコマンドの引数解析、`--format`（`text` / `json` / `github`）、`--diff`、`--stdout`、`--config`、`--ignore`（複数指定可）、`--force`（`init` 用）の制御。`--diff` および `--stdout` 指定時はファイルを上書きせず、`--stdout` への複数ファイル指定や `--diff` 併用時は終了コード `2` とする。  
* **`internal/config`**: `--config` 明示読み込みおよび `.kiku.yaml`（`kiku.yaml`）の親ディレクトリ遡及探索（キャッシュ付）、`.kikuignore` 読み込み、厳格デコード（未知キー拒否）、デフォルト設定の部分上書きマージ、バリデーション。また、グロブパターン（`*`、`?`、`**`、ディレクトリ指定）および `re:` 正規表現を評価し、正規化パスに対して判定を行う統一パスマッチャー（`PathMatcher`）を実装。  
* **`internal/engine`**: 複数ファイルの並列処理（`runtime.NumCPU()` 上限のワーカープール）、`internal/fs` を介したシンボリックリンク非追従のディレクトリ探索（ディレクトリ走査時は `.yaml` / `.yml` 大文字小文字不問、直接指定ファイルは拡張子不問）、ファイル単位のパイプラインオーケストレーション、決定論的な診断ソート（ファイルパス → 行番号 → 列番号 → ルールID順）。ルール単位の除外（`rules.<rule>.ignore`）は各ルール実行直前に相対パスに対して `RuleMatcher.Match(relPath)` を評価し、一致した場合は該当ルールのみスキップする。  
* **`internal/parser`**: `goccy/go-yaml`（`parser.ParseComments`）を用いた AST 構築、重複キー検出（`ast.MappingNode` 走査）、複数行リテラル（`|` / `>`）および複数行クォート文字列の保護行範囲抽出、および `# kiku:ignore` / `# kiku-disable` 系ディレクティブのパース。`IgnoreMap` の構築においては、行末インライン、AST ノードの開始行〜終了行（ブロック全体のインデント範囲）、空行までの段落スパン（`# kiku:ignore-section`）、および `disable`〜`enable` 区間を解決し、さらに制御コメント自体を含む行を `line-length` 判定から除外するメタデータを付与する。  
* **`internal/rules`**: 独立したルールインターフェース（`Rule`）を実装する各検証・修正ロジック群。  
* **`internal/formatter`**: コメントと空行を一切失わないためのテキスト・トークン再構築エンジン、および修正後 YAML のラウンドトリップ等価性検証器（`VerifyRoundTrip`）。  
* **`internal/fs`**: OS 差異吸収（パス正規化、シンボリックリンク非追従のファイル探索ヘルパー、改行コード変換、パーミッション保持、同一ディレクトリ一時ファイル＋`os.Rename` によるアトミック書き込み）。

# **2\. ディレクトリ構成**

```
kiku/
├── AGENTS.md                        # 全エージェント共通マスター定義（実体）
├── CLAUDE.md                        # AGENTS.md へのハードリンク
├── GEMINI.md                        # AGENTS.md へのハードリンク
├── .claude/
│   └── settings.json                # Claude Code 設定（Stop フック）
├── .codex/
│   └── config.toml                  # Codex CLI 設定（workspace-write / offline）
├── .gemini/
│   └── settings.json                # Gemini CLI / Antigravity 設定（AfterAgent フック）
├── go.mod                           # module github.com/withnic/kiku (Go 1.23+)
├── go.sum                           # github.com/goccy/go-yaml のみ依存
├── docs/
│   ├── 01_project_overview.md
│   ├── 02_data_modeling.md
│   └── 03_architecture.md
├── cmd/
│   └── kiku/
│       └── main.go                  # エントリーポイント（os.Exit 制御）
├── internal/
│   ├── cli/
│   │   ├── app.go                   # サブコマンドディスパッチ
│   │   ├── app_test.go
│   │   └── reporter.go              # text / json / github 出力フォーマット
│   ├── config/
│   │   ├── config.go                # Config 構造体・デフォルト値・探索ロジック
│   │   └── config_test.go
│   ├── parser/
│   │   ├── parser.go                # goccy/go-yaml ラッパー・重複キー・無視コメント解析
│   │   └── parser_test.go
│   ├── rules/
│   │   ├── rule.go                  # Rule インターフェース・レジストリ
│   │   ├── hygiene.go               # encoding, line-endings, line-length, trailing-ws, final-nl, blank-lines, no-tabs
│   │   ├── structure.go             # sequence-style, quotes, truthy, null-style, indentation, comments, document-start, sort-keys
│   │   └── rules_test.go
│   ├── formatter/
│   │   ├── rewriter.go              # コメント・空行保持型 AST/トークン再構築エンジン
│   │   ├── verify.go                # ラウンドトリップ等価性検証（データ値＋コメント不変性チェック）
│   │   └── formatter_test.go
│   ├── engine/
│   │   ├── runner.go                # 並列ワーカープール・グロブ除外・結果集約
│   │   └── runner_test.go
│   └── fs/
│       ├── atomic.go                # アトミック書き込み・パーミッション保持・CRLF/LF処理
│       └── atomic_test.go
└── testdata/
    ├── valid/                       # 正常系フィクスチャ（コメント・空行・複雑構造）
    ├── invalid/                     # 異常系フィクスチャ（タブ・重複キー・不正UTF-8）
    └── golden/                      # Fix 前後のゴールデンテストペア（*.in.yaml / *.out.yaml）
```

# **3\. コメント・空行完全保持の実装アーキテクチャ**

Google の `go-yaml` や単純な AST `String()` 再シリアライズでは、元のファイルに含まれていた空行やインラインコメントの微妙な位置が失われる。これを防ぐため、**「ソーススパン置換（Span-based Rewriting）＋ 構造再構築のハイブリッド方式」**を採用する。

1. **Pre-Scan（Layer 1 テキスト前処理・行数不変）**:  
   * 不正 UTF-8 検知、BOM 検知（内部処理用にメモリ上除去）、CRLF → LF 内部正規化、行頭インデントのハードタブ検知（`fix` 時のみ `indentation.spaces` 分の半角スペースへ内部置換して後続パースを救済）を実行する。  
   * この段階では行数を増減させる変更や Layer 2 書き換えを行わないため、後続のパース・診断における行番号は元ファイルと 1:1 で一致する。  
2. **AST パース & 保護領域抽出**:  
   * `goccy/go-yaml`（`parser.ParseComments`）で AST を構築し、構文エラー・重複キー検査、`IgnoreMap` 構築、および**複数行リテラルブロック（`|` / `>`）とクォートされた複数行文字列の内部行範囲**を特定する。  
   * **重要制約**: 複数行リテラルブロック（`|` や `>`）およびクォートされた複数行文字列の内部では、`trailing_whitespace`、`blank_lines`、`comments`、`line_length` を絶対に書き換えない（値そのものが変化してしまうため）。  
3. **AST スパン書き換え & Layer 2 衛生整形**:  
   * `goccy/go-yaml` の AST ノードが持つ `GetToken().Position`（行・列）とノード終端位置を特定し、`sequence-style`（`[a, b]` ↔ `- a`）、`quotes`、`truthy`、`null-style`、`indentation`、`comments`、`document-start`、`sort-keys` を**変更が必要なノードのテキスト範囲（スパン）のみ局所的に差し替える**。  
   * 保護領域および `IgnoreMap`・`rules.<rule>.ignore` を尊重した上で、`trailing-whitespace`、`blank-lines`、`final-newline` の行レベル整形を適用する。これにより、変更対象外の行にあるコメント、空行、セクション区切り装飾（`# === Section ===` 等）が 1 バイトも狂わずそのまま保持される。  
   * **スパン衝突・重なり解決ルール**: 同一行や親子ノード間で複数の書き換えスパンが発生する場合（例：`sequence-style` と `quotes` の同時適用）は、ボトムアップ（最深部の子ノードから親ノードへ）の順序でスパン変換を適用し、オフセットずれを防ぐ。  
4. **ラウンドトリップ等価性検証（`VerifyRoundTrip`）**:  
   * 変換後の YAML テキストを再度 `goccy/go-yaml` でパースし、以下を検証する。  
     1. パースエラーが発生しないこと。  
     2. すべてのスカラー値・マップ構造・配列順序が元の AST と意味的に一致すること（例外：`sort-keys` 有効時のマップキー順変更、および `truthy` 有効時の非クォート YAML 1.1 由来リテラル `yes`/`no`/`on`/`off` 等から `true`/`false` への正規化は許容する）。  
     3. 先頭 `#` を除き前後空白をトリムしたコメント本文について、その個数と出現順序が元の AST と完全一致すること（ただし `sort-keys` 有効時はキー移動に伴うコメント追従を許容するため、出現順序ではなくコメント本文ごとの出現回数マルチセットと総数の一致で検証する）。

# **4\. OS / 環境差異の吸収戦略（Windows 11 / WSL2 / Linux）**

* **パス区切りとグロブ・パスフィルタリング**: 内部の ignore パターンマッチングはすべて `internal/fs` で `filepath.ToSlash()` により `/` に統一して評価し、Windows と Linux/WSL2 で同一の除外結果になることを保証する。また、ディレクトリ走査時（`filepath.WalkDir`）にディレクトリ自体がグローバル `ignore` に一致した場合は `filepath.SkipDir` を返して配下の走査自体を打ち切り、高速化を図る。  
* **Windows でのアトミック書き込みとファイルロック**:  
  * Windows では書き込み先ファイルが開かれている場合や読み取り専用属性がある場合に `os.Rename` が失敗することがあるため、同一ディレクトリ内に `os.CreateTemp(dir, ".kiku-tmp-*")` で一時ファイルを作成・`Sync()`・`Close()` した上で `os.Rename` を実行する。エラー時は必ず `defer` で一時ファイルを `os.Remove` して残骸を残さない。  
  * 元のファイルの `fs.FileMode`（パーミッション）を引き継ぐ。  
* **改行コード**: 内部処理はすべて LF（`\n`）に正規化して行い、最終出力直前に `line_endings` ルールが有効かつ当該ファイルに適用される場合は設定値（`"crlf"` なら `\r\n`、`"lf"` なら `\n`）へ一括変換する。`line_endings` が無効または当該ファイルが除外対象の場合は元の改行コードを維持する（`OriginalEOL == "\r\n"` なら `\r\n`、それ以外は `\n`）。  
* **Windows 実行バイナリ拡張子とシンボリックリンク除外**: Windows 環境では出力バイナリが `bin/kiku.exe` となること、およびディレクトリ再帰探索時にシンボリックリンク・ジャンクションは無限ループ防止のため辿らない（`os.Lstat` 判定）ことを明記する。

# **5\. テスト戦略**

* **ゴールデンテスト（`testdata/golden/*.in.yaml` → `*.out.yaml`）**: 複雑なコメント配置（行頭・行末・配列要素間・EOF直前）、空行、複数行ブロックリテラル（`|` / `>`）、混在配列を含む実戦的な YAML を用意し、1文字の狂いもなく期待通り整形されることを検証する。  
* **冪等性テスト（Idempotency）**: すべてのゴールデンテストおよびフィクスチャに対して `Fix(Fix(input)) == Fix(input)`（2回連続で実行しても出力が完全に同一であること）を自動検証する。  
* **データ破壊防止テスト**: 構文エラー・重複キー・不正 UTF-8 を含むファイルに対して `kiku fix` を実行した際、元のファイル内容とタイムスタンプが一切変更されないことを検証する。  
* **レースコンディション検証**: `go test -race ./...` を標準検証コマンドとし、並列ワーカー実行時のデータ競合ゼロを担保する。

