# kpot ユーザーマニュアル

> **バージョン**: 0.4.0-dev  
> **最終更新**: 2026-04-26

---

## 目次

1. [概要](#1-概要)
2. [インストール](#2-インストール)
3. [クイックスタート](#3-クイックスタート)
4. [コマンドリファレンス](#4-コマンドリファレンス)
   - [init](#41-init)
   - [REPL モード](#42-repl-モード)
   - [シングルショットモード](#43-シングルショットモード)
   - [REPL コマンド詳細](#44-repl-コマンド詳細)
5. [ノート名のルール](#5-ノート名のルール)
6. [テンプレートとプレースホルダー](#6-テンプレートとプレースホルダー)
7. [設定ファイル](#7-設定ファイル)
8. [環境変数](#8-環境変数)
9. [ファイルフォーマット](#9-ファイルフォーマット)
10. [セキュリティ設計](#10-セキュリティ設計)
11. [リカバリーキー](#11-リカバリーキー)
12. [OS キーチェーン連携](#12-os-キーチェーン連携)
13. [アイドルロック](#13-アイドルロック)
14. [同期について](#14-同期について)
15. [実証ログ](#15-実証ログ)

---

## 1. 概要

`kpot`（key pot）は**暗号化された CLI ノート保管庫**です。  
API キー・パスワード・SSH 接続情報・秘密メモなどをひとつの `.kpot` ファイルに安全に保存します。

### 特徴

| 機能 | 詳細 |
|------|------|
| 暗号化方式 | Argon2id (64 MiB / 3 / 1) + XChaCha20-Poly1305 |
| 改ざん検出 | ヘッダー全体を AAD として AEAD で保護 |
| 原子書き込み | `.tmp` → `.bak` → 本体 の 2-phase rename |
| ノート編集 | `$EDITOR` 連携、一時ファイルは `/dev/shm` に配置 |
| TAB 補完 | TTY 接続時はコマンド名・ノート名を補完（liner） |
| クリップボード | コピー後 30 秒で自動消去（設定変更可） |

---

## 2. インストール

### 前提条件

- Go 1.18 以上
- Linux / macOS / Windows

### ビルド手順

```bash
git clone https://github.com/Shin-R2un/kpot.git
cd kpot
make build       # ./kpot を生成
make test        # 全テスト実行
make install     # $(go env GOPATH)/bin/kpot にインストール
```

---

## 3. クイックスタート

```bash
# 1. 新しい保管庫を作成（パスフレーズを設定）
kpot init personal.kpot

# 2. 保管庫を開いて REPL に入る
kpot personal.kpot

# 3. REPL 内で操作
kpot:personal> note ai/openai    # $EDITOR でノートを作成・編集
kpot:personal> ls                # ノート一覧
kpot:personal> read ai/openai   # ノートを表示
kpot:personal> copy ai/openai   # クリップボードにコピー（30秒で自動消去）
kpot:personal> find openai      # 名前・本文でキーワード検索
kpot:personal> rm ai/openai     # ノートを削除（確認あり）
kpot:personal> help             # コマンド一覧
kpot:personal> exit             # 終了
```

---

## 4. コマンドリファレンス

### 4.1 `init`

```
kpot init <file>
```

新しい保管庫ファイルを作成します。パスフレーズを 2 回入力して確認します。

- 既存ファイルへの上書きは拒否されます
- **パスフレーズを忘れると復元は不可能です**

```bash
$ kpot init personal.kpot
New passphrase: ********
Repeat passphrase: ********
Created personal.kpot
Keep this passphrase safe — there is no recovery if you lose it.
```

---

### 4.2 REPL モード

```
kpot <file>
```

保管庫を開いて対話型シェル（REPL）に入ります。

- TTY 接続時: **TAB 補完**（コマンド名・ノート名）と**矢印キー履歴**が使用可能
- Ctrl-C: 入力中の行を破棄してプロンプトに戻る（REPL 終了にはならない）
- Ctrl-D / `exit`: 保管庫を閉じて終了

```
kpot:personal> [TAB]
copy       exit       export     find       help       import
ls         note       passphrase quit       read       rm
template
```

---

### 4.3 シングルショットモード

```
kpot <file> <command> [args...]
```

REPL を開かずに 1 コマンドだけ実行します。スクリプトや他ツールとの連携に便利です。

```bash
# ノート一覧
kpot personal.kpot ls

# ノートを読む
kpot personal.kpot read ai/openai

# クリップボードにコピー
kpot personal.kpot copy ai/openai

# 検索
kpot personal.kpot find ssh

# 削除（確認スキップ）
kpot personal.kpot rm -y old/key

# JSON でエクスポート（stdout）
kpot personal.kpot export

# パスフレーズ変更
kpot personal.kpot passphrase
```

---

### 4.4 REPL コマンド詳細

#### `ls` — ノート一覧

```
ls
```

保管庫内のすべてのノート名をアルファベット順で表示します。

```
kpot:personal> ls
ai/anthropic
ai/openai
server/fw0
```

---

#### `note` — ノートの作成・編集

```
note <name>
```

`$EDITOR` を起動してノートを作成または編集します。

- **新規作成時**: テンプレートと作成日時のフロントマターが挿入されます
- **編集時**: 既存の本文の上に、作成日時・更新日時の両方を含むフロントマターが再生成されます（毎回 JSON メタデータから取り直されるため、編集中の表示は常に最新）
- フロントマター（`---` で囲まれた部分）は保存時に自動的に除去されます
- 本文が空またはテンプレート未変更の場合は保存されません

```
kpot:personal> note ai/openai
```

エディタに表示される内容（例）:

```markdown
---
created: 2026-04-25T12:00:00+09:00
updated: 2026-04-25T12:00:00+09:00
---

# ai/openai

- id:
- url:
- password:
- api_key:

## memo
```

保存後のノート本文（フロントマターは除去）:

```
# ai/openai

- id:
- url: https://platform.openai.com
- password:
- api_key: sk-xxxxxxxxxxxx

## memo
2026-04-25 発行
```

---

#### `read` — ノートの表示

```
read <name>
```

ノートの本文を標準出力に表示します。

```
kpot:personal> read ai/openai
# ai/openai

- api_key: sk-xxxxxxxxxxxx
...
```

---

#### `copy` — クリップボードへコピー

```
copy <name>
```

ノートの本文をシステムクリップボードにコピーし、設定された時間（デフォルト 30 秒）後に自動消去します。

- ユーザーが別のものをコピーした場合は自動消去しません
- Linux: `wl-copy`（Wayland）/ `xclip` / `xsel` を自動検出
- macOS: `pbcopy`
- Windows: PowerShell

```
kpot:personal> copy ai/openai
copied ai/openai via xclip (auto-clears in 30s)
```

---

#### `find` — 検索

```
find <query>
```

ノート名と本文をケースインセンシティブで検索します。

```
kpot:personal> find openai
ai/openai                        (name+body)  OPENAI_API_KEY=sk-xxx...
```

| タグ | 意味 |
|------|------|
| `(name)` | ノート名にのみマッチ |
| `(body)` | 本文にのみマッチ |
| `(name+body)` | 両方にマッチ |

---

#### `rm` — ノートの削除

```
rm [-y|--yes] <name>
```

ノートを削除します。`-y` を指定しない場合は確認プロンプトが表示されます。

```
kpot:personal> rm server/old
remove note "server/old"? [y/N]: y
removed server/old

kpot:personal> rm -y server/old2
removed server/old2
```

---

#### `template` — テンプレート管理

新規ノート作成時に挿入されるテンプレートを管理します。

| コマンド | 説明 |
|----------|------|
| `template` | テンプレートを `$EDITOR` で編集して保存 |
| `template show` | 現在のテンプレートを表示 |
| `template reset` | 組み込みデフォルトに戻す |

テンプレートは保管庫ファイル内に暗号化されて保存されるため、保管庫ごとに異なるテンプレートを持てます。

---

#### `passphrase` — パスフレーズ変更

```
passphrase
```

保管庫のパスフレーズを変更します。

- 新しい salt で鍵を再導出し、ファイルを再暗号化します
- **`.bak` ファイルを削除します**（旧パスフレーズのバックアップを残さないため）

```
kpot:personal> passphrase
New passphrase: ********
Repeat: ********
passphrase changed; previous .bak removed
```

> ⚠️ `KPOT_PASSPHRASE` 環境変数が設定されている場合、新旧両プロンプトに同じ値が使われます。TTY で直接実行することを推奨します。

---

#### `export` — JSON エクスポート

```
export [-o <path>] [--force]
```

保管庫の全ノートを**平文 JSON** として出力します。

| オプション | 説明 |
|-----------|------|
| （省略） | stdout に出力 |
| `-o <path>` | ファイルに書き込む |
| `--force` | 既存ファイルへの上書きを許可 |

> ⚠️ 出力は暗号化されていません。ファイルに書き込んだ場合は使用後に削除してください。

```bash
# stdout に出力（他コマンドへのパイプに便利）
kpot personal.kpot export | jq '.notes | keys'

# ファイルに書き込み
kpot personal.kpot export -o backup.json

# 既存ファイルを上書き
kpot personal.kpot export -o backup.json --force
```

---

#### `import` — JSON インポート

```
import <json-file> [--mode merge|replace] [-y|--yes]
```

`export` で出力した JSON ファイルからノートを読み込みます。

| オプション | デフォルト | 説明 |
|-----------|-----------|------|
| `--mode merge` | ✅ | 新規ノートを追加。既存ノートとの衝突は `.conflict-YYYYMMDD` に保存 |
| `--mode replace` | | 既存ノートをすべて置換（要確認または `-y`） |
| `-y` / `--yes` | | 確認プロンプトをスキップ |

```bash
# マージ（安全、衝突は別名で保存）
kpot personal.kpot import backup.json

# 全置換（確認あり）
kpot personal.kpot import backup.json --mode replace

# 全置換（確認スキップ）
kpot personal.kpot import backup.json --mode replace -y
```

---

## 5. ノート名のルール

| ルール | 詳細 |
|--------|------|
| 使用可能文字 | `a-z`, `0-9`, `-`, `_`, `.`, `/` |
| 大文字 | 自動的に小文字に変換 |
| 最大長 | 128 文字 |
| 階層区切り | `/`（例: `ai/openai`, `server/fw0`） |
| 禁止パターン | 先頭・末尾の `/`、連続した `//` |

```bash
# 有効な名前
ai/openai
server/fw0.example
my-secret_key.prod

# 無効な名前
/leading-slash    # 先頭スラッシュ
trailing/         # 末尾スラッシュ
a//b              # 連続スラッシュ
has space         # スペース
```

---

## 6. テンプレートとプレースホルダー

新規ノート作成時、テンプレートに以下のプレースホルダーを埋め込めます。  
プレースホルダーは**ノート作成時に一度だけ展開**されます（編集時には再展開されません）。

| プレースホルダー | 展開される値 |
|----------------|-------------|
| `{{name}}` | ノートの完全な名前（例: `ai/openai`） |
| `{{basename}}` | 最後の `/` 以降（例: `openai`） |
| `{{date}}` | 作成日（例: `2026-04-25`） |
| `{{time}}` | 作成時刻（例: `14:30`） |
| `{{datetime}}` | ISO 8601 形式（例: `2026-04-25T14:30:00+09:00`） |

**テンプレート例**:

```markdown
# {{basename}}

- url:
- username:
- password:
- created: {{date}}

## memo
```

---

## 7. 設定ファイル

`~/.config/kpot/config.toml`（存在しない場合はデフォルト値を使用）

```toml
# $EDITOR / $VISUAL より優先されるエディタ
editor = "vim"

# クリップボードの自動消去秒数（デフォルト: 30）
clipboard_clear_seconds = 60

# OS キーチェーンに鍵をキャッシュするポリシー
#   "auto"   : 初回 unlock 時に [Y/n] 確認 (デフォルト)
#   "always" : 確認なしで毎回キャッシュ
#   "never"  : 完全に無効化
keychain = "auto"

# REPL アイドルロックの分数 (デフォルト: 10)
# REPL でこの分数の間入力がないと自動的に vault を閉じてプロセス終了
idle_lock_minutes = 10
```

| キー | デフォルト | 説明 |
|------|-----------|------|
| `editor` | `""` | エディタコマンド。空の場合は `$EDITOR` → `$VISUAL` → `nano/vim/vi` の順にフォールバック |
| `clipboard_clear_seconds` | `30` | 0 はデフォルト値（30秒）を使用。負の値はエラー |
| `keychain` | `"auto"` | OS キーチェーンに鍵をキャッシュするポリシー。`auto` / `always` / `never` 以外はエラー |
| `idle_lock_minutes` | `10` | REPL のアイドルロック分数。0 はデフォルト値 (10) を使用。負の値はエラー |

---

## 8. 環境変数

| 変数 | 説明 |
|------|------|
| `KPOT_PASSPHRASE` | TTY プロンプトの代わりにパスフレーズとして使用される。設定時は毎回 stderr に警告が表示される（1プロセスにつき1回） |
| `EDITOR` | 使用するエディタ（設定ファイルの `editor` より低優先） |
| `VISUAL` | `EDITOR` が未設定の場合に使用 |

> ⚠️ `KPOT_PASSPHRASE` は本番環境での常時設定は推奨しません。シェル履歴や環境変数リストからパスフレーズが漏洩するリスクがあります。

---

## 9. ファイルフォーマット

`.kpot` ファイルは JSON 形式です。

```json
{
  "format": "kpot",
  "version": 1,
  "kdf": {
    "name": "argon2id",
    "salt": "<base64, 16 bytes>",
    "params": {
      "memory_kib": 65536,
      "iterations": 3,
      "parallelism": 1
    }
  },
  "cipher": {
    "name": "xchacha20-poly1305",
    "nonce": "<base64, 24 bytes>"
  },
  "payload": "<base64 ciphertext>"
}
```

`payload` を復号すると以下の構造の JSON が現れます：

```json
{
  "version": 1,
  "created_at": "2026-04-25T12:00:00Z",
  "updated_at": "2026-04-25T12:00:00Z",
  "template": "<任意：per-vault な新規ノートテンプレ。未設定なら省略>",
  "notes": {
    "ai/openai": {
      "body": "...",
      "created_at": "...",
      "updated_at": "..."
    }
  }
}
```

`template` フィールドは `template` コマンドで設定された場合のみ含まれ、保管庫ファイルごと（ヘッダー外）に暗号化されて保管されます。

### ファイルの世代管理

保存のたびに以下の手順で原子的に書き込まれます：

```
1. <file>.tmp を書き込み → fsync
2. <file> → <file>.bak にリネーム（存在する場合）
3. <file>.tmp → <file> にリネーム
4. ディレクトリを fsync
```

クラッシュが起きても `<file>` か `<file>.bak` のどちらかが必ず残ります。

---

## 10. セキュリティ設計

### 暗号化

| 要素 | 仕様 |
|------|------|
| KDF | Argon2id — 64 MiB メモリ、3 反復、並列度 1 |
| 暗号 | XChaCha20-Poly1305（24 バイトノンス、書き込みごとに新規生成） |
| 鍵長 | 32 バイト |
| 塩長 | 16 バイト（`crypto/rand`） |
| AAD | KDF セクションと暗号セクションを含む JSON ヘッダー |

### 改ざん検出

ヘッダー全体（KDF パラメータ・salt・cipher 名・nonce）が AEAD の AAD として暗号文に束縛されます。  
KDF パラメータの変更（ダウングレード攻撃）や salt の入れ替えは復号時に `ErrAuthFailed` となります。

### メモリ保護

- パスフレーズ・鍵・平文はスコープ終了時に `crypto.Zero()` でゼロ埋め（ベストエフォート — Go の GC が `[]byte` を内部で move する可能性があるため完全な拭き取りは保証されない）
- `mlock` による swap 抑止は **未実装**。同一権限で動く悪意あるプロセスからのメモリ読取・物理的にディスクへ swap される攻撃は脅威モデル外
- エディタ一時ファイルは Linux では `/dev/shm`（tmpfs）に配置
- 一時ファイルはゼロ埋めしてから `unlink`
- パスフレーズ比較は `crypto/subtle.ConstantTimeCompare` でタイミング攻撃を防止

### クリップボード

- コピー後に設定時間（デフォルト 30 秒）でクリア
- ユーザーが別のものをコピーした場合はクリアしない（上書きを避ける）
- セッション終了時（`Close()`）に即時クリア

---

## 11. リカバリーキー

v0.3 以降の `kpot init` は **必ずリカバリーキーを発行**し、initの実行直後に1度だけ画面表示します。再発行できないので、その場で紙やオフライン媒体にメモしてください。

```
kpot init personal.kpot
                  → 12-word seed (BIP-39, default)
kpot init personal.kpot --recovery key
                  → 32-byte secret key (Crockford Base32 表記)
kpot init personal.kpot --recovery seed --recovery-words 24
                  → 24-word seed (256-bit)
```

| flag | 結果 | 用途 |
|---|---|---|
| (省略) | BIP-39 12-word seed | 紙メモ向き |
| `--recovery seed --recovery-words 24` | BIP-39 24-word seed | より強い entropy |
| `--recovery key` | 32-byte secret key (52字 base32) | パスマネ paste 向き |

設計上の制約:
- **再発行不可**: 一度発行されたら永久に固定。漏れたら新しい vault を作って `export → import` で移行
- **Vault 寿命中固定**: passphrase を rotate しても recovery は変わらない (DEK 不変設計)
- **TTY 必須**: stdin/stdout が pipe/redirect だと init は実行を拒否し vault 作成も rollback。CI ログに seed が漏れない

復旧フロー:
```
kpot personal.kpot --recover
  → recovery key prompt → unlock → REPL に入る
kpot:personal> passphrase
  → 新パスフレーズ設定 (recovery key は保持)
kpot:personal> exit
```

種類確認:
```
kpot personal.kpot recovery-info
  → "Recovery: enabled (type: seed-bip39)" など (KDF params は表示しない)
```

セキュリティ:
- 表示は `/dev/tty` 直書き。stderr/stdout には**書かない**ので tmux scrollback / log capture / CI artifact に残らない
- 表示後 ENTER 待機 → ANSI clear-screen
- 誤 recovery 入力 vs 改ざんは同一エラー文 (情報漏洩防止)

---

## 12. OS キーチェーン連携

v0.4 以降、初回の vault unlock 時に「キャッシュしますか? [Y/n]」と聞かれます。Y を選ぶと OS-native のシークレットストアに**鍵を保存**し、以降の `kpot <file>` は passphrase prompt と Argon2id 派生 (~100ms) を skip します。

```
kpot personal.kpot
Passphrase: ********
Cache key in OS keychain so future opens skip the passphrase? [Y/n]: y
Opened personal.kpot

# 次回以降:
kpot personal.kpot ls
ai/openai
server/fw0
```

バックエンド:

| OS | 利用するもの | 必要なもの |
|---|---|---|
| macOS | `/usr/bin/security` (Keychain Services) | macOS 同梱、追加インストール不要 |
| Linux | `secret-tool` (libsecret) | `apt install libsecret-tools` または `dnf install libsecret`、加えて D-Bus session bus が必要 |
| Windows | `wincred` (Credential Manager) | Windows 同梱 |

**第三者 Go 依存ゼロ** — kpot は zalando/go-keyring 等のライブラリを使わず、各 OS provider の CLI / syscall を直接叩きます。supply chain の trust 範囲が増えません。

設定とフラグ:

| 操作 | 動作 |
|---|---|
| `kpot <file>` | キャッシュ済みの鍵があれば即時 unlock。無ければ passphrase prompt + (`auto` モード時) キャッシュ確認 |
| `kpot <file> --no-cache` | キャッシュ参照と保存をスキップ |
| `kpot <file> --forget` | 現 vault の cache 削除して exit (subcommand を続けるとそれを no-cache で実行) |
| `kpot keychain test` | バックエンド診断 (利用可能か、現在のモード) |

`config.toml` の `keychain = "always"` で確認なしキャッシュ、`"never"` で完全無効化。

バージョン跨ぎの整合性:
- **v1 vault**: passphrase rotate で派生鍵が変わる → cache 自動失効
- **v2 vault**: passphrase rotate で DEK 不変 → cache はそのまま有効 (これが v0.3 envelope 設計の見返り)

特殊ケース:
- `KPOT_PASSPHRASE` セット時はキャッシュ参照も書込みもスキップ (CI 汚染防止)
- リカバリー fallback (`--recover`) はキャッシュに触れない (緊急用なので silent caching を避ける)
- Linux で D-Bus session 未起動 / `secret-tool` 不在 → silent fallback で passphrase prompt
- **macOS の既知制約**: `/usr/bin/security add-generic-password -w <hex>` で hex 化した鍵が argv に乗る。Big Sur 以降の `ps` は同 UID 内に制限されるので keychain 参照と同じ脅威境界。気になれば `keychain = "never"`

---

## 13. アイドルロック

REPL に入った後、設定された分数の間 1コマンドも入力されないと kpot は自動的に vault を閉じプロセスを終了します。デフォルト 10分、`config.toml` の `idle_lock_minutes` で変更可能。

```
$ kpot personal.kpot
Opened personal.kpot (3 notes)
kpot:personal>
   ... 10分間アイドル ...
(idle timeout — vault locked)
$
```

仕様:
- TTY 接続時のみ有効 (heredoc / pipe テストでは無効化)
- コマンド実行 / Ctrl-C / 空 ENTER でタイマー reset
- タイマー発火時は `crypto.Zero` で鍵を wipe してから `os.Exit(0)`
- 単発コマンド (`kpot <file> ls` 等) には影響しない (REPL に入らないため)

無効化は `idle_lock_minutes` を非常に大きい値 (例: `525600` で1年) に設定するか、`KPOT_PASSPHRASE` で REPL 不使用にするしかありません (idle ロックの目的上、完全無効化は意図的にサポートしていません)。

---

## 14. 同期について

`kpot` は**同期 transport を持ちません**。これは設計判断であり、欠落ではありません。

`.kpot` は1ファイル完結の暗号化ブロブなので、ユーザーは以下のうち好みの transport を**そのまま**使えます。

| 方法 | 例 |
|---|---|
| Git | `git add personal.kpot && git push` |
| クラウドストレージ | Google Drive / Dropbox / iCloud Drive 上に置く |
| 同期ツール | Syncthing / rsync / Resilio Sync |
| 物理メディア | USB / SD カードに `cp` |

### 注意点

- **毎 save で payload 全体が再暗号化される**: 1つの note を変更しただけでも ciphertext は丸ごと差し替わる。これは「変更内容を漏らさない」という設計上の特性であり、副作用として **git diff は意味を持たない**（履歴サイズは膨らむ）
- **conflict 解決は手動**: 別端末で同じ vault を別々に編集してから sync すると、ciphertext は merge できない。「このマシンが正」運用にする
- **`.bak` を同期に含めない**: `*.bak` を `.gitignore` / 同期除外に入れる
- **同時編集対策はまだ無い**: 同一マシンで2つの REPL を同時に開く事故防止 (`<file>.lock`) は v0.5 で追加予定

### v0.5 で追加予定: `kpot merge`

transport は引き続き kpot の責務外。ただし transport-agnostic な vault プリミティブとして以下を v0.5 に予定：

- `kpot merge a.kpot b.kpot -o merged.kpot` — 両 vault を復号 → notes をマージ → 衝突は別キーで残す → 再暗号化
- `<file>.lock` でローカル並行操作を防止
- 暗号化 payload に optional な `device_id` / `parent_save_id` を追加し、merge 自動化を補助

これにより、git だろうと Drive だろうと、衝突した2ファイルが手元に揃えば1コマンドで解決できる。

---

## 15. 実証ログ

以下は sandbox 環境での実際の動作ログです。

### テスト結果

```
$ go test ./... -count=1

ok  github.com/r2un/kpot/internal/clipboard   (7 tests)
ok  github.com/r2un/kpot/internal/config      (5 tests)
ok  github.com/r2un/kpot/internal/crypto      (5 tests)
ok  github.com/r2un/kpot/internal/editor      (3 tests)
ok  github.com/r2un/kpot/internal/notefmt     (14 tests)
ok  github.com/r2un/kpot/internal/repl        (11 completion + 22 repl tests)
ok  github.com/r2un/kpot/internal/store       (9 tests)
ok  github.com/r2un/kpot/internal/tty         (2 tests)
ok  github.com/r2un/kpot/internal/vault       (8 tests)

全 86 テスト PASS ✅
```

### コマンド実証

```bash
# 保管庫の作成
$ KPOT_PASSPHRASE=demopass kpot init demo.kpot
Created demo.kpot
-rw------- 507 bytes  ← パーミッション 600

# ノートの作成（3件）
$ KPOT_PASSPHRASE=demopass kpot demo.kpot note ai/openai
$ KPOT_PASSPHRASE=demopass kpot demo.kpot note ai/anthropic
$ KPOT_PASSPHRASE=demopass kpot demo.kpot note server/fw0

# 一覧・読み取り・検索
$ KPOT_PASSPHRASE=demopass kpot demo.kpot ls
ai/anthropic
ai/openai
server/fw0

$ KPOT_PASSPHRASE=demopass kpot demo.kpot read ai/openai
OPENAI_API_KEY=sk-demo-1234567890abcdef
...

$ KPOT_PASSPHRASE=demopass kpot demo.kpot find api
ai/anthropic  (body)  ANTHROPIC_API_KEY=sk-ant-demo-9876543210
ai/openai     (body)  OPENAI_API_KEY=sk-demo-1234567890abcdef

# 平文が vault ファイルに含まれないことを確認
$ grep "sk-demo" demo.kpot
(0 件 — 暗号化されている)

# エクスポート → 削除 → マージインポート
$ KPOT_PASSPHRASE=demopass kpot demo.kpot export -o /tmp/backup.json
$ KPOT_PASSPHRASE=demopass kpot demo.kpot rm -y server/fw0
removed server/fw0
$ KPOT_PASSPHRASE=demopass kpot demo.kpot import /tmp/backup.json
merged: +1 new, 2 conflicts renamed (.conflict-YYYYMMDD)
$ KPOT_PASSPHRASE=demopass kpot demo.kpot ls
ai/anthropic
ai/anthropic.conflict-20260425  ← 衝突ノート（安全に保存）
ai/openai
ai/openai.conflict-20260425
server/fw0                      ← 復元成功

# パスフレーズ変更
$ KPOT_PASSPHRASE=demopass kpot demo.kpot passphrase
passphrase changed; previous .bak removed
$ ls demo.kpot*
demo.kpot   ← .bak は削除済み（旧パスのバックアップを残さない）

# 誤パスフレーズ拒否（exit code 3）
$ KPOT_PASSPHRASE=wrongpass kpot demo.kpot ls
Wrong passphrase, or the file is corrupted
exit code: 3

# ヘッダー改ざん検出（AAD バインディング）
$ python3 -c "...iterations を 3→1 に改ざん..."
$ KPOT_PASSPHRASE=demopass kpot /tmp/tampered.kpot ls
Wrong passphrase, or the file is corrupted
exit code: 3   ← 改ざんを検出
```

---

*このマニュアルは kpot v0.2.0-dev に基づいています。*
