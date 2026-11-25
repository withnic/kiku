![規矩（kiku） — コメントと空行を完全保持する YAML Linter & Fixer](docs/assets/kiku.jpg)

# 規矩（kiku）

**コメントと意図的な空行を完全に保持したまま** YAML の構文検証（Lint）と人間が読みやすい形式への自動整形（Fix）を行う、Go 製の単一バイナリ CLI ツールです。

- **ゼロデータロス設計**: AST スパン単位の局所書き換えとラウンドトリップ等価性検証（`VerifyRoundTrip`）により、コメント・空行・複数行リテラル（`|` / `>`）を壊さず、同一ディレクトリの一時ファイル経由でアトミックに書き戻します。
- **2 層検証パイプライン**: 仕様違反（Layer 1・常時有効）とスタイルルール（Layer 2・すべて個別にオン/オフ可能）を分離。構文エラーや重複キーのあるファイルは `fix` でも絶対に上書きしません。
- **クロスプラットフォーム＆日本語対応**: Windows / WSL2 / Linux / macOS で同一の判定・出力を実現。UTF-8 マルチバイト（日本語・絵文字・Unicode エスケープ・BOM/CRLF）を安全に扱います。
- **高速な並列実行**: ワーカープール並列処理とディレクトリ単位の事前除外（`filepath.SkipDir`）により、大規模な YAML 群も高速に処理します。

## 特徴（Before / After）

```yaml
# Before
## === CI Workflow ===
name: 'ci'
on:
  push:
    branches: [main, release] # kiku:ignore sequence-style -- keep compact
  pull_request:
    types: [opened, synchronize]
env:
  APP_PORT: "08080"
  FEATURE_FLAG: YES
  EMPTY_VAL: null #inline note
```

```yaml
# After (`kiku fix`)
## === CI Workflow ===
name: ci
on:
  push:
    branches: [main, release]  # kiku:ignore sequence-style -- keep compact
  pull_request:
    types:
      - opened
      - synchronize
env:
  APP_PORT: "08080"
  FEATURE_FLAG: true
  EMPTY_VAL:  # inline note
```

- 装飾コメント（`## === ... ===`）、インラインコメント、意図的な空行、複数行リテラル内部をそのまま保持します。
- 数値文字列（`"08080"`）や予約語文字列（`"yes"`）のクォート、マッピングのキー（`on:` 等）は誤解釈を防ぐため自動保護します。

## インストール

Go 1.23+ が必要です。

### `go install`

```bash
go install github.com/withnic/kiku/cmd/kiku@latest
```

### ソースからビルド

```bash
git clone https://github.com/withnic/kiku.git
cd kiku
go build -o bin/kiku ./cmd/kiku
```

Windows（PowerShell）の場合:

```powershell
go build -o bin/kiku.exe ./cmd/kiku
```

## クイックスタート

```bash
# 1. カレントディレクトリにデフォルトの .kiku.yaml を生成（任意・ゼロコンフィグでも動作します）
kiku init

# 2. YAML ファイルを検査（違反があれば終了コード 1）
kiku check

# 3. 差分を確認（元ファイルは変更しません）
kiku fix --diff

# 4. 自動整形を適用
kiku fix
```

## コマンドリファレンス

```text
kiku <check|fix|init|version> [options] [paths...]
```

### `kiku check [options] [paths...]`

YAML ファイルを検証し、違反一覧を標準出力へ出力します（ファイルは書き換えません）。

| オプション | 説明 | デフォルト |
| :--- | :--- | :--- |
| `--format <type>` | 出力形式（`text` / `json` / `github`） | `text` |
| `--config <path>` | 設定ファイルを明示指定 | 自動探索 |
| `--ignore <pattern>` | 除外パターンを追加（複数回指定可、グロブ / `re:<regex>`） | なし |

```bash
# カレントディレクトリ（または .kiku.yaml の include）を検査
kiku check

# 複数ディレクトリ・ファイルを指定して JSON 出力
kiku check configs/ manifests/app.yaml --format json

# 特定パターンを除外して GitHub Actions 形式で出力
kiku check --format github --ignore "*.generated.yaml" --ignore "re:^tmp/"
```

### `kiku fix [options] [paths...]`

YAML ファイルを自動整形します。構文エラー・重複キー・不正 UTF-8 などの修復不能な Layer 1 エラーを含むファイルは上書きせず、標準エラー出力へ報告して終了コード `1` を返します。

| オプション | 説明 | デフォルト |
| :--- | :--- | :--- |
| `--diff` | Unified Diff を標準出力へ出力（元ファイルは上書きしない） | `false` |
| `--stdout` | 単一ファイルの整形結果を標準出力へ出力（元ファイルは上書きしない） | `false` |
| `--config <path>` | 設定ファイルを明示指定 | 自動探索 |
| `--ignore <pattern>` | 除外パターンを追加（複数回指定可） | なし |

> `--diff` と `--stdout` の同時指定、または `--stdout` で対象ファイルが 1 件以外になった場合は終了コード `2` になります。

```bash
# 差分プレビュー
kiku fix --diff configs/

# 単一ファイルの整形結果を標準出力へ
kiku fix --stdout config.yaml

# ファイルを安全なアトミック書き込みで整形
kiku fix configs/
```

### `kiku init [options]`

カレントディレクトリにコメント付きの標準 `.kiku.yaml` を生成します。

| オプション | 説明 | デフォルト |
| :--- | :--- | :--- |
| `--force` | 既存の `.kiku.yaml` を上書きする（未指定時は既存ファイルがあると終了コード `2`） | `false` |

### `kiku version`

バージョン情報を表示します（例: `kiku v0.1.0`）。

## 対象ファイルと除外指定

- **対象指定の優先順位**: CLI 引数 `[paths...]` ＞ `.kiku.yaml` の `include` ＞ カレントディレクトリ `.`。
- **対象拡張子**: ディレクトリ走査時は `.yaml` / `.yml`（大文字小文字不問）を対象とします。CLI 引数でファイルパスを直接指定した場合は拡張子に関わらず対象とします。
- **シンボリックリンク**: 無限ループ防止のため、シンボリックリンクおよびディレクトリジャンクションは追従しません。
- **全体除外**: 以下の 3 箇所が統合され、ファイル探索段階で除外されます（ディレクトリ一致時は配下走査をスキップ）。
  1. `.kiku.yaml` のトップレベル `ignore`
  2. `.kiku.yaml` と同じディレクトリ（未配置時はカレントディレクトリ）の `.kikuignore`（1 行 1 パターン、空行・`#` コメント行は無視）
  3. CLI の `--ignore <pattern>`
- **ルール別除外**: `.kiku.yaml` の `rules.<rule>.ignore` に記述すると、該当ルールのみ対象ファイルから除外できます。
- **パターン記法**: `/` 区切りのグロブ（`*`, `?`, `**` 対応）または `re:<Go正規表現>` プレフィックスが使えます。

## 設定ファイル（`.kiku.yaml`）

`--config` 未指定時は、対象ファイルのディレクトリから親ディレクトリへ遡って `.kiku.yaml`（同一ディレクトリに無ければ `kiku.yaml`）を探索し、見つからない場合は組み込みデフォルト設定を使用します。未知のキーや不正な値はタイポ防止のため設定エラー（終了コード `2`）となります。

```yaml
version: 1

include:
  - .

ignore:
  - .git/**
  - node_modules/**
  - vendor/**

rules:
  encoding:
    enabled: true
    forbid_bom: true
    ignore: []

  line_endings:
    enabled: true
    type: lf                      # "lf" | "crlf"
    ignore: []

  line_length:
    enabled: true
    max: 120                      # UTF-8 ルーン数ベース
    ignore_comments: true
    ignore_urls: true
    ignore: []

  trailing_whitespace:
    enabled: true
    ignore: []

  final_newline:
    enabled: true
    ignore: []

  blank_lines:
    enabled: true
    max_consecutive: 1
    ignore: []

  indentation:
    enabled: true
    spaces: 2                     # 1〜8
    ignore: []

  sequence_style:
    enabled: true
    mode: block                   # "block" | "flow" | "consistent" | "hybrid"
    allow_empty_flow: true
    hybrid_max_items: 3
    hybrid_max_line_length: 60
    ignore: []

  quotes:
    enabled: true
    prefer: double                # "double" | "single"
    unnecessary: remove           # "remove" | "keep"
    ignore: []

  truthy:
    enabled: true
    style: lower                  # "lower"
    ignore: []

  null_style:
    enabled: true
    style: empty                  # "empty" | "null" | "tilde"
    ignore: []

  comments:
    enabled: true
    require_space_after_hash: true
    min_spaces_from_content: 2
    ignore: []

  document_start:
    enabled: false
    present: false                # true: "---" 必須 / false: "---" 禁止
    ignore: []

  sort_keys:
    enabled: false
    recursive: true
    ignore: []
```

## ルール一覧

### Layer 1（YAML 1.2 仕様準拠・常時有効・無効化不可）

| RuleID | 内容 | `kiku fix` 時の挙動 |
| :--- | :--- | :--- |
| `invalid-utf8` | 不正な UTF-8 バイト列を検知 | 非破壊スキップ（エラー終了 `1`） |
| `no-tabs` | 行頭インデント内のハードタブ（`\t`）を検知 | `indentation.spaces` 分の半角スペースへ変換して救済 |
| `syntax-error` | YAML 1.2 構文エラー・未定義エイリアスを検知 | 非破壊スキップ（エラー終了 `1`） |
| `key-duplicates` | 同一マッピング内の重複キーを検知 | 非破壊スキップ（エラー終了 `1`） |

### Layer 2（スタイル・可読性・全ルール個別オン/オフ可能）

| RuleID | デフォルト | 自動修正 | 概要 |
| :--- | :---: | :---: | :--- |
| `encoding` | 有効 | 可 | UTF-8 BOM の検知・除去 |
| `line-endings` | 有効 | 可 | 改行コードを `lf` / `crlf` に統一（無効・除外時は元の改行コードを維持） |
| `line-length` | 有効 | 不可 | 1 行の最大文字数チェック（UTF-8 ルーン数、コメント・URL・制御コメント除外対応） |
| `trailing-whitespace` | 有効 | 可 | 行末空白・タブの除去（複数行リテラル・複数行クォート内部は保護） |
| `final-newline` | 有効 | 可 | 非空ファイル末尾の単一改行を保証（余剰末尾空行を整理、0 バイト空ファイルは許容） |
| `blank-lines` | 有効 | 可 | 連続空行を `max_consecutive` 以下に圧縮 |
| `indentation` | 有効 | 可 | インデント幅を `spaces` に統一 |
| `sequence-style` | 有効 | 可 | 配列スタイル統一（`block` / `flow` / `consistent` / `hybrid`、コメント内包配列・空配列保護） |
| `quotes` | 有効 | 可 | 不要クォート除去・優先クォート統一（空文字・予約語・数値文字列・構文文字・前後空白・制御文字を保護） |
| `truthy` | 有効 | 可 | 非クォート真偽値（`yes`/`no`/`on`/`off`/`True`/`FALSE` 等）を `true`/`false` に統一（マッピングキー・1 文字 `y`/`n` は対象外） |
| `null-style` | 有効 | 可 | Null 表記を `empty` / `null` / `tilde` に統一（`empty` はブロックマッピング値のみ安全適用） |
| `comments` | 有効 | 可 | `#` 後の半角スペース強制（`##` 連続装飾保護）・インラインコメント前スペース統一 |
| `document-start` | 無効 | 可 | 第 1 ドキュメント先頭 `---` の必須化または禁止（第 2 ドキュメント以降の区切りは保護） |
| `sort-keys` | 無効 | 可 | マッピングキーの昇順ソート（直上コメント追従・複数行リテラル対応） |

## インライン無視ディレクティブ

YAML 内のコメントで、Layer 2 ルールのみ 4 つの粒度で部分無効化できます（`-- 理由` の付記、複数ルールのカンマ／空白区切り、`snake_case` 指定にも対応）。制御コメント行自体は `line-length` 判定から自動除外されます。

```yaml
# 1. 行末インライン（同一行のみ）
items: [a, b, c]  # kiku:ignore sequence-style -- 意図的に横並び

# 2. 次の行・ブロック全体（ネストした子要素まで自動追従）
# kiku:ignore
legacy_service:
    flags: [YES, 'kept']

# 3. 段落単位（次の空行・ドキュメント区切り ---・EOF まで）
# kiku:ignore-section quotes, truthy
param_a: 'keep'
param_b: ON

# 4. 範囲指定（enable まで継続。kiku-disable / kiku-enable も使用可）
# kiku:disable line-length
very_long_key: "..."
# kiku:enable line-length
```

## 終了コード

| コード | 意味 | 発生条件 |
| :---: | :--- | :--- |
| `0` | 正常終了 | `check` で違反 0 件、または `fix` が完了（修復不能な Layer 1 エラーなし） |
| `1` | Lint / 構文エラーあり | `check` で 1 件以上の違反を検出、または `fix` 対象に修復不能な Layer 1 エラーあり |
| `2` | 実行時・設定エラー | CLI 引数・フラグ不正、`.kiku.yaml` / `.kikuignore` 不正、I/O エラー、ラウンドトリップ検証失敗 |

## CI 連携（GitHub Actions 例）

```yaml
name: yaml-lint
on: [push, pull_request]
jobs:
  kiku:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.23"
      - run: go install github.com/withnic/kiku/cmd/kiku@latest
      - run: kiku check --format github
```

## 開発・テスト

```bash
# フォーマット・静的解析
gofmt -s -w . && go vet ./...

# 単体・結合テスト（レース検出付）
go test -v -race ./...

# バイナリビルド
go build -o bin/kiku ./cmd/kiku
```

設計ドキュメントは [docs/](docs/) に格納しています。

## ライセンス

[MIT License](LICENSE)

