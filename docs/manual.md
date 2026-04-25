# kpot ユーザーマニュアル

> **バージョン**: 0.2.0-dev  
> **最終更新**: 2026-04-25

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
11. [実証ログ](#11-実証ログ)

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
copy       exit       find       help       import     ls
note       passphrase quit       read       rm         template
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
- **編集時**: 既存の本文と更新日時のフロントマターが表示されます
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
```

| キー | デフォルト | 説明 |
|------|-----------|------|
| `editor` | `""` | エディタコマンド。空の場合は `$EDITOR` → `$VISUAL` → `nano/vim/vi` の順にフォールバック |
| `clipboard_clear_seconds` | `30` | 0 はデフォルト値（30秒）を使用。負の値はエラー |

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

- パスフレーズ・鍵・平文はスコープ終了時に `crypto.Zero()` でゼロ埋め
- エディタ一時ファイルは Linux では `/dev/shm`（tmpfs）に配置
- 一時ファイルはゼロ埋めしてから `unlink`
- パスフレーズ比較は `crypto/subtle.ConstantTimeCompare` でタイミング攻撃を防止

### クリップボード

- コピー後に設定時間（デフォルト 30 秒）でクリア
- ユーザーが別のものをコピーした場合はクリアしない（上書きを避ける）
- セッション終了時（`Close()`）に即時クリア

---

## 11. 実証ログ

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
