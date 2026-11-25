# Phase 2: YAML スタイルルール・コメント完全保持フォーマッタ・ラウンドトリップ検証

## 実行前提・行動規範（厳守）
- 本タスクは自律型コーディングエージェント（Codex CLI / Antigravity / Gemini CLI / `claude -p` ＋ `ccw`）により完全非対話で実行されている。ユーザーへの質問・確認・選択の提示は一切行わず、`AGENTS.md`（ハードリンク先の `CLAUDE.md` / `GEMINI.md` 含む）および `docs/` 配下の設計書に従って最も安全かつ仕様に忠実な判断を下し、実装を完了させること。
- ローカルファイル操作・ビルド・テストコマンドの範囲内に厳密にとどめ、`git push` や許可外のシステム変更は絶対に行わないこと。

## 参照ドキュメント
実装前に必ず以下のファイルをすべて読み込み、仕様を完全に把握すること。
- `AGENTS.md`
- `docs/01_project_overview.md`
- `docs/02_data_modeling.md`
- `docs/03_architecture.md`

## Phase 1 からの引き継ぎ事項（既存実装の前提）
Phase 1 では以下が実装・検証済み（`go test -race ./...` グリーン）。既存コードを確認した上で、これらと整合する形で Phase 2 を積み上げること。
- **`internal/fs`**: `AtomicWrite`（既存ファイル前提のアトミック置換・パーミッション維持）、`NormalizePath`、`RelativePath`、`WalkYAML`（シンボリックリンク非追従・ルートディレクトリ保護済）、`NormalizeEOL` / `OutputEOL` / `ApplyEOL` / `LineEndings` を提供。
- **`internal/config`**: `Config` / `Rules` / `Defaults()` / `Resolve()` / `Loader`（親遡及探索キャッシュ、`--config`、`.kikuignore`、AST ベース厳格デコード、全フィールドバリデーション）および `PathMatcher`（グロブ `**`・`re:` 対応）を提供。各ルールの有効・除外判定は `(*config.Resolved).RuleEnabled(ruleID, relativePath)` を使用する。
- **`internal/parser`**: `Parse(path, raw, fix, indentSpaces)` が Layer 1 検査（`invalid-utf8`, `no-tabs`, `syntax-error`, `key-duplicates`、アンカー/エイリアスのドキュメント境界検査）と `Document`（`AST`, `IgnoreMap`, `Protected(line)`, `QuotedStart(line)`, `NormalizedText`, `Lines`）を構築する。`fix=true` 時はインデントタブ救済後の `NormalizedText` 上で `AST` を構築しているため、AST トークン位置は `doc.NormalizedText` 基準である。
- **`internal/rules`**: `rule.go` の `Rule` インターフェースは現在 `hygienePlan` 専用（`plan(*hygienePlan)`）となっているため、Phase 2 の `structure.go` 追加時に衛生ルールと構造ルールの両方を自然に扱える形へ拡張・整理すること。また `Hygiene` は元行番号ベースで一括変換し、`final-newline` 有効時の末尾空行と `blank-lines` の重複診断を防止済み。
- **パイプライン接続上の注意**:
  - `docs/03_architecture.md` 3節の順序に従い、AST 構造ルール（`structure.go`）＋スパン書き換え（`formatter/rewriter.go`）とテキスト衛生整形（`Hygiene`）を組み合わせる際は、元行番号と `IgnoreMap`・複数行スカラー保護領域がずれないように設計し、最終的な書き換え結果を必ず `formatter.VerifyRoundTrip` に通してから出力すること。
  - `github.com/goccy/go-yaml`（`v1.19.2` がキャッシュ済み）のクォートトークンは `Origin` に前置空白・改行を含む場合があるため、スパン計算時は実トークン位置（`Position.Offset` / `Line` / `Column`）と `Origin` の挙動を `go doc` および既存 `internal/parser/parser.go` を参考に確認すること。

## Phase 2 実装タスク
1. **YAML スタイルルール群（`internal/rules/structure.go`）の実装**:
   - `sequence-style`: `block` / `flow` / `consistent` / `hybrid`（`allow_empty_flow`, `hybrid_max_items`, `hybrid_max_line_length` 対応、空配列 `[]` の非破壊処理、コメント内包配列の保護）を実装する。
   - `quotes`: `prefer`（`double` / `single`）および `unnecessary`（`remove` / `keep`）を実装し、`docs/02_data_modeling.md` 2.4節の全6項目のクォート保護ルール（空文字、真偽値/Null予約語、数値文字列、YAML構文記号・フロー内記号、前後空白、改行・制御文字）を厳密に遵守する。
   - `truthy`: 非クォートの `True`/`TRUE`/`False`/`FALSE` および YAML 1.1 由来リテラル（`yes`/`Yes`/`YES`/`no`/`No`/`NO`/`on`/`On`/`ON`/`off`/`Off`/`OFF`）を小文字 `true`/`false` へ統一する（クォート済みおよび1文字 `y`/`n` は除外）。
   - `null-style`: `empty` / `null` / `tilde` の統一を実装する（`empty` はブロックマッピング値のみ適用し、フローコレクション内・キー・配列要素等での構文破壊を防止）。
   - `indentation`: 指定スペース幅へのインデント統一を実装する。
   - `comments`: `#` 後の半角スペース強制（`##` 等の連続装飾シャープ保護）およびインラインコメント前の最小スペース数統一を実装する。
   - `document-start`（デフォルトオフ、ファイル先頭の第1ドキュメント開始 `---` のみ対象とし第2ドキュメント以降の区切りは保護）および `sort-keys`（デフォルトオフ、コメント追従ソート対応）を実装する。
2. **コメント・空行完全保持型スパン書き換えエンジン（`internal/formatter/rewriter.go`）の実装**:
   - AST ノードの位置情報（スパン）をもとに変更対象ノードのみをボトムアップ順で局所書き換えし、行頭コメント・行末コメント・セクション装飾・空行を 1 バイトも失わずに再構築する処理を実装する。
3. **ラウンドトリップ等価性検証器（`internal/formatter/verify.go`）の実装**:
   - 変換後の YAML を再パースし、構文妥当性・データ値等価性（`sort-keys` 有効時のキー順変更と `truthy` 有効時の正規化を許容）・コメント本文（先頭 `#` 除去＋前後空白トリム後）の個数および出現順序（`sort-keys` 有効時はマルチセット一致）の不変性を検証し、不一致時はエラーを返して元ファイルを保護する仕組みを実装する。
4. **ゴールデンテスト・冪等性テストの作成**:
   - `testdata/golden/` に複合的なテストケース（混在配列、各種クォート保護、インラインコメント、空行、マルチドキュメント、無視ディレクティブ）を作成し、`Fix(Fix(x)) == Fix(x)` の冪等性テストを含めてすべてパスさせる。ゴールデンテストに「ブロック丸ごとスキップ（`# kiku:ignore` でネストしたマップ・配列全体を維持）」および「段落スキップ（`# kiku:ignore-section` で次の空行までの複数キーをまとめて維持）」のケースを追加する。

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
- 全検証がグリーンになったことを確認した上で、作成したファイル一覧・テスト結果・Phase 3 への引き継ぎ事項を簡潔に出力して終了すること。
