# **02\_data\_modeling — 規矩（kiku）**

# **1\. 設定ファイルスキーマ（`.kiku.yaml`）**

設定ファイルは `.kiku.yaml`（フォールバックとして `kiku.yaml`）を使用する。存在しない場合はすべての項目にデフォルト値が適用される。

```yaml
# .kiku.yaml スキーマ定義とデフォルト値
version: 1

# 検査・修正の対象パス（未指定時はカレントディレクトリ "." 全体を走査）
include:
  - "."

# 全体除外パターン（グロブ指定 / "re:..." プレフィックスで正規表現指定が可能）
# デフォルトは以下の3パターン。プロジェクト固有の除外は追加記述する
ignore:
  - ".git/**"
  - "node_modules/**"
  - "vendor/**"
  # 記述例:
  # - "*.generated.yaml"
  # - "re:^build/.*\\.ya?ml$"

rules:
  # --- テキスト・ファイル衛生ルール（オンオフ可能） ---
  encoding:
    enabled: true
    forbid_bom: true              # UTF-8 BOM の検知・除去
    ignore: []                    # ルール固有の除外パターン（デフォルトは空配列）

  line_endings:
    enabled: true
    type: "lf"                    # "lf" | "crlf"
    ignore: []

  line_length:
    enabled: true
    max: 120                      # 1行の最大文字数（ルーン数ベース）
    ignore_comments: true         # コメント行を文字数制限から除外
    ignore_urls: true             # http:// または https:// を含む行を除外
    ignore: []                    # 記述例: ["templates/*.yaml"]

  trailing_whitespace:
    enabled: true                 # 行末の空白・タブ除去
    ignore: []

  final_newline:
    enabled: true                 # ファイル末尾の単一改行を保証
    ignore: []

  blank_lines:
    enabled: true
    max_consecutive: 1            # 連続する空行の最大許容数（0で全空行削除、1で1行まで維持）
    ignore: []

  # --- YAML スタイル・可読性ルール（オンオフ可能） ---
  indentation:
    enabled: true
    spaces: 2                     # インデント幅（2 または 4 推奨）
    ignore: []

  sequence_style:
    enabled: true
    mode: "block"                 # "block" | "flow" | "consistent" | "hybrid"
    allow_empty_flow: true        # block / hybrid / consistent でも空配列 [] を許可する
    hybrid_max_items: 3           # mode: hybrid 時の最大要素数
    hybrid_max_line_length: 60    # mode: hybrid 時のフロースタイル最大文字数（スカラー要素のみ対象）
    ignore: []                    # 記述例: ["docker-compose*.yml", "re:^manifests/legacy/.*$"]

  quotes:
    enabled: true
    prefer: "double"              # "double" | "single"
    unnecessary: "remove"         # "remove"（不要なら外す） | "keep"（既存クォート維持）
    ignore: []

  truthy:
    enabled: true
    style: "lower"                # "lower"（true / false に統一）
    ignore: []

  null_style:
    enabled: true
    style: "empty"                # "empty"（key:） | "null"（key: null） | "tilde"（key: ~）
    ignore: []

  comments:
    enabled: true
    require_space_after_hash: true # "#comment" -> "# comment"（"##" 等の装飾連続シャープは保護）
    min_spaces_from_content: 2    # インラインコメント前の最小スペース数（"val # c" -> "val  # c"）
    ignore: []

  document_start:
    enabled: false                # デフォルトオフ
    present: false                # true: "---" 必須 / false: "---" 禁止
    ignore: []

  sort_keys:
    enabled: false                # デフォルトオフ
    recursive: true               # ネストしたマッピングも再帰ソートするか
    ignore: []
```

## **1.1 設定バリデーション・探索・除外規約**

* 未知のフィールドが `.kiku.yaml` に含まれる場合は設定エラー（終了コード `2`）とし、タイポによる設定無視を防ぐ。  
* **部分上書きマージ**: `.kiku.yaml` で一部項目のみ記述された場合、未指定フィールド（特にデフォルト `true` の真偽値や数値）がゼロ値で上書きされないよう、組み込みデフォルト値を初期値として部分マージする。  
* **値域・列挙型バリデーション**（違反時は設定エラー終了コード `2`）:  
  * `version`: 省略時は `1` として扱い、指定時は `1` のみ許可。  
  * `line_endings.type`: `"lf"`, `"crlf"` のいずれか。  
  * `line_length.max`: `>= 1` の整数。  
  * `blank_lines.max_consecutive`: `>= 0` の整数。  
  * `indentation.spaces`: `1 <= spaces <= 8` の整数。  
  * `sequence_style.mode`: `"block"`, `"flow"`, `"consistent"`, `"hybrid"` のいずれか。  
  * `sequence_style.hybrid_max_items`: `>= 1` の整数、`hybrid_max_line_length`: `>= 1` の整数。  
  * `quotes.prefer`: `"double"`, `"single"` のいずれか、`quotes.unnecessary`: `"remove"`, `"keep"` のいずれか。  
  * `truthy.style`: `"lower"` のみ。  
  * `null_style.style`: `"empty"`, `"null"`, `"tilde"` のいずれか。  
  * `comments.min_spaces_from_content`: `>= 1` の整数。  
* **パターンマッチング**: パターン文字列が `re:` で始まる場合は Go 標準の `regexp.Compile` でコンパイルし、構文不正な正規表現が設定されている場合は設定エラー（終了コード `2`）とする。`re:` 以外の場合はパス区切りを `/` に正規化したグロブパターン（`**` 再帰マッチ対応）として評価する。  
* **`.kikuignore` ファイル**: `.kiku.yaml` と同じディレクトリ（設定ファイル不在時はカレントディレクトリ）に `.kikuignore` が存在する場合は読み込み、トップレベル `ignore` にマージする。1行1パターンで記述し、空行および `#` 開始行（コメント）は無視する（書式はグロブおよび `re:` に対応）。  
* **対象ファイル拡張子**: ディレクトリ走査時は拡張子 `.yaml` / `.yml`（大文字小文字不問）のファイルのみを対象とする。CLI 引数でファイルパスを直接指定した場合は拡張子に関わらず対象とする。  
* **設定探索と優先順位**: `--config <path>` 指定時はそのファイルのみを使用する（不在・不正なら終了コード `2`）。未指定時は、`include` 解決のためカレントディレクトリから親へ遡って `.kiku.yaml`（同一ディレクトリに無ければ `kiku.yaml`）を探索し、各ファイル処理時は対象ファイルの親ディレクトリから最も近い設定ファイルを探索・キャッシュして適用する。  
* **グローバル除外とルール別除外**: グローバル `ignore`（`.kiku.yaml` / `.kikuignore` / `--ignore` の統合）はファイル探索段階で対象外とし、各ルール配下の `rules.<rule_name>.ignore` は該当ルールのチェックおよび Fix 適用時のみ対象ファイルをスキップする（他のルールは通常通り適用される）。  
* **CLI 引数優先**: CLI 引数でパスが渡された場合（例：`kiku check dirA/ fileB.yaml`）は設定ファイルの `include` よりも CLI 引数を優先し、CLI フラグ `--ignore`（複数指定可）はグローバル `ignore` に追加マージされる。  
* **設定キーと RuleID の対応**: `.kiku.yaml` のキーは `snake_case`（例: `sequence_style`）、診断結果 `Diagnostic.RuleID` およびインラインディレクティブの正規名は `kebab-case`（例: `sequence-style`）とする。インラインディレクティブでは利便性のため `snake_case` 指定も `kebab-case` に正規化して受理する。

# **2\. コアデータ構造（Go 型定義）**

## **2.1 診断結果（Diagnostic）**

```
type Severity string

const (
    SeverityError   Severity = "error"   // Layer 1 仕様違反・構文エラー
    SeverityWarning Severity = "warning" // Layer 2 スタイル・可読性ルール違反
)

type Diagnostic struct {
    FilePath string   `json:"file_path"`
    Line     int      `json:"line"`      // 1-indexed
    Column   int      `json:"column"`    // 1-indexed（UTF-8 ルーン単位）
    RuleID   string   `json:"rule_id"`   // 例: "sequence-style", "no-tabs", "key-duplicates"
    Severity Severity `json:"severity"`
    Fixable  bool     `json:"fixable"`   // kiku fix で自動修正可能か
    Message  string   `json:"message"`
}
```

## **2.2 処理対象ドキュメントモデル（Document）**

```
type Document struct {
    Path            string       // 正規化済みファイルパス
    RawBytes        []byte       // 元の生バイト列（BOM・改行コード検査用）
    HadBOM          bool         // UTF-8 BOM の有無
    OriginalEOL     string       // "\n" | "\r\n" | "mixed"
    NormalizedText  string       // UTF-8 BOM除去・LF正規化済みテキスト
    Lines           []string     // 行単位スライス（末尾改行除く）
    IgnoreMap       *IgnoreMap   // # kiku-disable 系ディレクティブの有効範囲マップ
    AST             *ast.File    // goccy/go-yaml パース済み AST（コメント付）
}
```

## **2.3 配列スタイル判定・変換ルール詳細**

* **空配列 `[]` の扱い（YAML 1.2 制約）**: YAML の構文上、空配列はフロースタイル `[]` でしか表現できない（ブロックスタイルに書き換えると `null` になり型が変わる）。そのため、`allow_empty_flow: true`（デフォルト）の場合は `block` / `hybrid` / `consistent` のいずれでも空配列 `[]` を許容・維持する。`allow_empty_flow: false` の場合は `check` で警告（`Fixable: false`）を報告するが、`fix` 時はデータ破壊を防ぐため自動変換せず維持する。  
* **`block`**: すべての非空配列をブロックスタイル（`- item`）に変換する。  
* **`flow`**: すべての非空配列をフロースタイル（`[item1, item2]`）に変換する。ただし、配列要素内にコメントが含まれる場合はコメント消失を防ぐため変換をスキップし警告のみ出力する（`Fixable: false`）。  
* **`consistent`**: ドキュメント内で最初に出現した非空配列のスタイル（`block` または `flow`）を基準とし、異なるスタイルの非空配列を違反として検知・統一する（基準が `flow` の場合でも、コメント内包配列は変換をスキップし警告のみ出力する）。  
* **`hybrid`**: 非空配列のうち、以下の条件を**すべて**満たす配列は `flow`（`[a, b, c]`）、1つでも満たさない配列は `block`（`- a`）とする。  
  1. 要素数が `1` 以上 `hybrid_max_items`（デフォルト `3`）以下。  
  2. すべての要素がスカラー値（文字列・数値・真偽値・Null）であり、ネストした配列やマップを含まない。  
  3. 要素内および要素間にコメントが存在しない。  
  4. 1行のフロースタイルとして整形した際の文字数が `hybrid_max_line_length`（デフォルト `60`）以下。

## **2.4 クォート保護ルール（誤解釈防止の安全弁）**

`quotes.unnecessary: "remove"` が有効な場合でも、クォートを外すと YAML 1.2 の型解決や構文が変わってしまう以下の文字列は**絶対にクォートを維持**する。

1. 空文字列 `""` / `''`  
2. 真偽値・Null と解釈されるリテラル（大小文字問わず `true`, `false`, `yes`, `no`, `on`, `off`, `null`, `~`）  
3. YAML 1.2 の数値（整数・浮動小数点・符号付・指数表記・16進数 `0x...`・8進数 `0o...`・`.inf`・`+.inf`・`-.inf`・`.nan`）としてパース可能な文字列（例: `"0123"`, `"1.0"`, `"3.14"`, `"+1"`, `"1e5"`）  
4. YAML 構文文字を含む文字列（先頭の `-`, `?`, `:`, `,`, `[`, `]`, `{`, `}`, `#`, `&`, `*`, `!`, `|`, `>`, `'`, `"`, `%`, `@`, `` ` ``、文字列中のコロン＋空白 `": "` または末尾の `":"`、文字列中の空白＋シャープ `" #"`。さらにフローコレクション内部の要素では `,`, `[`, `]`, `{`, `}` を含む文字列も保護する）  
5. 先頭または末尾に空白文字を含む文字列（例: `" foo "`）  
6. 改行・制御文字（`\n`, `\r`, `\t` 等）を含む文字列（エスケープが必要なためダブルクォートを維持）

## **2.5 その他ルールの安全変換・エッジケース規定**

* **`truthy`**: クォートされていないプレーンスカラーのうち、`True` / `TRUE` / `False` / `FALSE` および YAML 1.1 由来の曖昧な真偽値リテラル（`yes` / `Yes` / `YES` / `no` / `No` / `NO` / `on` / `On` / `ON` / `off` / `Off` / `OFF`）を小文字の `true` / `false` へ統一する。クォート済み文字列（`"yes"` 等）および1文字スカラー（`y` / `n`）は誤変換防止のため対象外とする。  
* **`null-style`**: プレーンな Null リテラル（`null` / `Null` / `NULL` / `~` および空値 `key:`）を対象とする（クォート済み `"null"` / `"~"` は文字列のため対象外）。`style: "empty"` はブロックマッピングの値（`key:`）にのみ適用し、フローコレクション内（`[null]`, `{k: null}`）・マッピングキー・ブロック配列要素・ドキュメントルート単一スカラーでは YAML 構文破壊を防ぐため `empty` 化をスキップする。  
* **`document-start`**: ファイル先頭（第1ドキュメントの開始位置）の `---` のみを対象とする。マルチドキュメント YAML における第2ドキュメント以降の区切り `---` は、データ破壊を防ぐため `present: false` 時でも絶対に削除しない。  
* **`line-length`**: 自動折り返しは行わない（`Fixable: false`）。複数行リテラル（`|` / `>`）・複数行クォート文字列の内部行、および制御コメント行（`# kiku:...` 等）は常に判定から除外する。  
* **`final-newline` / `blank-lines`**: 空ファイル（0 バイト）は改行を追加せずそのまま許容する。非空ファイルの末尾に複数空行がある場合は `final-newline` により単一改行へ正規化する。  
* **`line-endings`**: ルールが有効かつ当該ファイルが除外対象でない場合は設定値（`lf` / `crlf`）へ統一する。無効または除外対象の場合は元の改行コードを維持する（`OriginalEOL == "\r\n"` なら CRLF、それ以外は LF）。  
* **`no-tabs`**: `indentation.enabled: false` の場合でも、`kiku fix` 時の行頭タブ救済には設定された `indentation.spaces`（デフォルト `2`）を使用する。

# **3\. インライン無視ディレクティブ仕様**

YAML ファイル内に制御コメントを記述することで、ファイル全体ではなく「特定の1行」「ブロック全体（ネストした子要素一式）」「段落（空行で区切られた塊）」「任意の範囲」単位で Layer 2 ルールのみ部分無効化できる（Layer 1 の仕様違反は無効化不可）。  
書きやすさを考慮し、`golangci-lint` ライクな短縮記法（`# kiku:ignore`）と明示記法（`# kiku-disable`）の両方をサポートする（`--` による理由コメント付与も許容：例 `# kiku:ignore sequence-style -- ここは意図的に横並び`）。複数ルールを指定する場合はカンマ区切りまたは空白区切りのいずれでも記述可能とする（下表の `[...]` は省略可能を示すメタ表記）。

## **3.1 無効化の4つの粒度（スコープ）**

1. **行末インライン指定（同一行・ノード対象）**:  
   * YAML コンテンツの行末に `# kiku:ignore [rule1, rule2]` を記述する（例: `items: [a, b, c]  # kiku:ignore sequence-style`）。  
   * その行（およびその行で開始する単一値・フロー配列/マップ）のみ無効化。  
2. **次の行・ブロック全体指定（ASTノード単位）**:  
   * 独立コメント行として直前の行に `# kiku:ignore [rule1, rule2]` または `# kiku-disable-next-line [rule1, rule2]`（ルール名省略時は全 Layer 2 ルール対象）を書く。  
   * **重要（ブロック自動追従）**: 直後の行が単一スカラーであればその1行のみ、直後の行が **マッピングの親キー（`services:`）や配列の親（`items:` / `- name:`）** である場合は、**インデントが戻ってそのブロックが終わるまで（子要素すべてを含む AST ノード全体）** を丸ごとスキップする。  
3. **段落（空行区切りセクション）指定**:  
   * `# kiku:ignore-section [rule1, rule2]`（エイリアス: `# kiku:ignore-paragraph`）  
   * このコメントが書かれた位置から、**次の空行（またはファイル末尾・ドキュメント区切り `---`）が出現するまでの「1段落（かたまり）」全体** を一括で無効化する。複数のトップレベルキーが連続して並んでいる設定の塊をまとめて飛ばしたいときに使う。  
4. **範囲指定（開始〜終了ペア）**:  
   * `# kiku:disable [rule1, rule2]`（エイリアス: `# kiku-disable`） 〜 `# kiku:enable [rule1, rule2]`（エイリアス: `# kiku-enable`）  
   * 明示的に解除コメントが現れるまで（またはファイル末尾まで）無効化を継続する。

## **3.2 ディレクティブ自体の保護ルール**

* 制御コメント（`# kiku:...` / `# kiku-disable...` / `# kiku-enable...`）を含む行は、`line-length` の文字数制限判定から自動的に除外し、制御コメントを書いたせいで文字数オーバーになる本末転倒を防ぐ。

## **3.3 マルチドキュメント YAML（`---` 区切り）の扱い**

* 1ファイル内に複数の YAML ドキュメント（`---` で区切られた複数ストリーム）が含まれる場合、各ドキュメント単位で `consistent` モード判定および AST 変換を実行し、ドキュメント区切りマーカー（`---` / `...`）とドキュメント間のコメントをそのまま維持する。

# **4\. 状態遷移と CLI 終了コード仕様**

## **4.1 処理パイプライン状態遷移**

```
[Raw File]
   │
   ├─(1) Pre-Scan: UTF-8 / BOM / EOL / Tab 検査 (行数不変)
   │      ├─ 不正 UTF-8 ──> [Error: invalid-utf8] (終了・Fix不可)
   │      └─ タブ混入 ───> check時: [Error: no-tabs] / fix時: スペース置換して継続
   │
   ├─(2) goccy/go-yaml パース (ParseComments) & 重複キー検査 & 保護領域・IgnoreMap 構築
   │      ├─ 構文エラー ──> [Error: syntax-error] (終了・Fix不可)
   │      └─ 重複キー ────> [Error: key-duplicates] (終了・Fix不可)
   │
   ├─(3) Layer 2 ルール評価 & ASTスパン / 衛生テキスト変換 (保護領域・IgnoreMap・ルール別ignore尊重)
   │
   ├─(4) ラウンドトリップ等価性検証 (Fix時のみ)
   │      ├─ 再パース失敗またはデータ値/コメント欠落 ──> [Internal Error] (元ファイル保護・書込中止)
   │      └─ 検証成功 ──> 改行コード適用 & 出力 (--diff / --stdout 非上書き、通常時は TempFile -> Rename)
```

## **4.2 CLI 終了コードおよび出力モード一覧**

* `kiku fix` において `--diff` または `--stdout` が指定された場合は、元ファイルを一切上書きしない（ドライラン出力）。`--stdout` に複数ファイルが対象となった場合、および `--diff` と `--stdout` を同時指定した場合は引数エラーとして終了コード `2` を返す。

| 終了コード | 意味 | 発生条件 |
| :---- | :---- | :---- |
| `0` | 正常終了 | `check` で違反 0 件、または `fix` で全ファイルの修正・検証が完了（残存する未修正 Layer 1 エラーなし） |
| `1` | Lint / 構文エラーあり | `check` で 1 件以上の違反を検出、または `fix` 対象に修正不能な Layer 1 エラー（構文エラー・重複キー等）が含まれる |
| `2` | 実行時・設定エラー | CLI 引数・フラグ組み合わせ不正、`.kiku.yaml` の構文/スキーマ不正、ファイル I/O エラー、ラウンドトリップ検証失敗 |

