# kpot ユーザーマニュアル

> **バージョン**: 0.5.0  
> **最終更新**: 2026-04-27  
> **リポジトリ**: https://github.com/Shin-R2un/kpot

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
   - [keychain サブコマンド](#45-keychain-サブコマンド)
5. [ノート名のルール](#5-ノート名のルール)
6. [テンプレートとプレースホルダー](#6-テンプレートとプレースホルダー)
7. [設定ファイル](#7-設定ファイル)
8. [環境変数](#8-環境変数)
9. [リカバリーキー](#9-リカバリーキー)
10. [OS キーチェーン連携](#10-os-キーチェーン連携)
11. [アイドルロック](#11-アイドルロック)
12. [バンドル転送 (.kpb)](#12-バンドル転送-kpb)
13. [ファイルフォーマット](#13-ファイルフォーマット)
14. [セキュリティ設計](#14-セキュリティ設計)
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
| リカバリーキー | BIP-39 シードフレーズ (12/24語) または Crockford Base32 秘密鍵 |
| DEK 分離 | v2 vault は DEK をパスフレーズ wrap + recovery wrap で二重保護 |
| OS キーチェーン | macOS / Linux (secret-tool) / Windows (Credential Manager) 対応 |
| 原子書き込み | `.tmp` → `.bak` → 本体 の 2-phase rename |
| アイドルロック | 無操作 N 分でセッション自動ロック (デフォルト 10 分) |
| ノート編集 | `$EDITOR` 連携、一時ファイルは `/dev/shm` に配置 |
| TAB 補完 | TTY 接続時はコマンド名・ノート名を補完 (liner) |
| クリップボード | コピー後 30 秒で自動消去 (設定変更可) |
| バンドル転送 | 選択したノートを `.kpb` ファイルに暗号化して共有 |

---

## 2. インストール

### ワンライナーインストール (Linux / macOS)

```bash
curl -fsSL https://raw.githubusercontent.com/Shin-R2un/kpot/main/install.sh | sh
```

### ワンライナーインストール (Windows PowerShell)

```powershell
irm https://raw.githubusercontent.com/Shin-R2un/kpot/main/install.ps1 | iex
```

### Go でビルド

```bash
git clone https://github.com/Shin-R2un/kpot.git
cd kpot
make build       # ./kpot を生成
make test        # 全テスト実行
make install     # $(go env GOPATH)/bin/kpot にインストール
```

### 動作要件

- Go 1.18 以上 (ビルド時)
- Linux / macOS / Windows
- クリップボード: `xclip` または `wl-clipboard` (Linux), `pbcopy` (macOS), PowerShell (Windows)
- OS キーチェーン (任意): `libsecret-tools` (Linux), macOS 標準, Windows 標準

---

## 3. クイックスタート

```bash
# 1. 新しい保管庫を作成 (リカバリーシードが表示される)
kpot init personal.kpot

# 2. 保管庫を開いて REPL に入る
kpot personal.kpot

# 3. REPL 内で操作
kpot:personal> note ai/openai      # $EDITOR でノートを作成・編集
kpot:personal> ls                  # ノート一覧
kpot:personal> read ai/openai      # ノートを表示
kpot:personal> copy ai/openai      # クリップボードにコピー (30秒で自動消去)
kpot:personal> find openai         # 検索
kpot:personal> rm ai/openai        # 削除 (確認あり)
kpot:personal> exit
```

---

## 4. コマンドリファレンス

### 4.1 init

```
kpot init <file> [--recovery seed|key] [--recovery-words 12|24]
```

新しい暗号化保管庫を作成します。**v0.3 以降は常にリカバリーキーが生成されます。**

| オプション | 説明 | デフォルト |
|-----------|------|----------|
| `--recovery seed` | BIP-39 ニーモニックシードフレーズを生成 | ✓ デフォルト |
| `--recovery key` | Crockford Base32 秘密鍵を生成 | — |
| `--recovery-words 12` | 12語のシード (128bit エントロピー) | ✓ デフォルト |
| `--recovery-words 24` | 24語のシード (256bit エントロピー) | — |

```bash
# デフォルト (12語シード)
kpot init personal.kpot

# 24語シード
kpot init personal.kpot --recovery seed --recovery-words 24

# 秘密鍵形式
kpot init personal.kpot --recovery key
```

> ⚠️ **リカバリーキーは init 時に一度だけ表示されます。必ず紙に書き留めてください。**  
> 紛失した場合、パスフレーズを忘れると vault は復元不能になります。

---

### 4.2 REPL モード

```
kpot <file> [--recover] [--no-cache] [--forget]
```

インタラクティブなシェルに入ります。

| オプション | 説明 |
|-----------|------|
| `--recover` | リカバリーキーでパスフレーズなしに開く |
| `--no-cache` | OS キーチェーンキャッシュを使わない |
| `--forget` | キャッシュされたキーを削除してから開く |

```bash
kpot personal.kpot            # 通常起動
kpot personal.kpot --recover  # リカバリーキーで起動
kpot personal.kpot --no-cache # キャッシュ無効
```

---

### 4.3 シングルショットモード

```
kpot <file> <command> [args...]
```

REPL に入らず、コマンドを 1 回だけ実行します。スクリプトや CI 向けです。

```bash
kpot personal.kpot ls
kpot personal.kpot read ai/openai
kpot personal.kpot rm -y old/key
kpot personal.kpot export -o backup.json --force
```

---

### 4.4 REPL コマンド詳細

#### `ls`
ノート一覧を表示します。

```
kpot:personal> ls
ai/anthropic
ai/openai
server/fw0
```

---

#### `note <name>`
ノートを `$EDITOR` で作成・編集します。  
テンプレートが適用され、フロントマター (作成日・更新日) が自動付与されます。

```
kpot:personal> note ai/openai
```

---

#### `read <name>`
ノートの本文を表示します。

```
kpot:personal> read ai/openai
OPENAI_API_KEY=sk-...
```

---

#### `copy <name>`
ノート本文をクリップボードにコピーします。  
デフォルト 30 秒後に自動消去されます (設定変更可)。

```
kpot:personal> copy ai/openai
copied ai/openai via xclip (auto-clears in 30s)
```

---

#### `find <query>`
ノート名・本文を大文字小文字を区別せずに検索します。

```
kpot:personal> find api
ai/openai   (body)  OPENAI_API_KEY=sk-...
```

---

#### `rm [-y] <name>`
ノートを削除します。`-y` で確認をスキップします。

```
kpot:personal> rm ai/openai
remove ai/openai? [y/N]: y
removed ai/openai
```

---

#### `passphrase`
パスフレーズを変更します。  
v2 vault では DEK は保持され、リカバリーキーは引き続き有効です。

```
kpot:personal> passphrase
New passphrase: 
Repeat: 
passphrase changed; previous .bak removed
```

---

#### `recovery-info`
リカバリーキーの種別を表示します (シークレットは表示しません)。

```
kpot:personal> recovery-info
Recovery: seed-bip39 (12 words)
```

---

#### `template` / `template show` / `template reset`
新規ノートのテンプレートを管理します。

```
kpot:personal> template show      # 現在のテンプレートを表示
kpot:personal> template           # $EDITOR でテンプレートを編集
kpot:personal> template reset     # 組み込みデフォルトに戻す
```

---

#### `export [-o <path>] [--force]`
復号した vault の内容を JSON で出力します。  
デフォルトは stdout。ファイルに書き出す場合は `--force` が必要です。

```
kpot:personal> export
kpot:personal> export -o backup.json --force
```

> ⚠️ ファイルに書き出した場合、平文 JSON がディスクに残ります。使用後は削除してください。

---

#### `import <file> [--mode merge|replace] [-y]`
JSON ファイルからノートをインポートします。

| オプション | 説明 |
|-----------|------|
| `--mode merge` | 既存ノートを保持し、新規ノートを追加 (デフォルト) |
| `--mode replace` | 全ノートを置き換え |
| `-y` | 確認をスキップ |

衝突したノートは `.conflict-YYYYMMDD` サフィックスで保存されます。

```
kpot:personal> import backup.json --mode merge
kpot:personal> import backup.json --mode replace -y
```

---

#### `bundle <name>... -o <path> [--force]`
選択したノートを暗号化 `.kpb` ファイルにエクスポートします。  
別パスフレーズで保護するため、受信者は元の vault のパスフレーズを知らなくても構いません。

```
kpot:personal> bundle ai/openai server/fw0 -o transfer.kpb
Bundle passphrase (recipient will need it): 
wrote 2 notes to transfer.kpb
note: share the passphrase via a separate channel
```

---

#### `import-bundle <path> [-y]`
`.kpb` ファイルを復号して現在の vault にマージします。

```
kpot:personal> import-bundle transfer.kpb
Source bundle passphrase: 
bundle contains 2 notes:
  ai/openai   OPENAI_API_KEY=sk-...
  server/fw0  Host: example.com
import 2 notes into this vault? [y/N]: y
imported: +2 new, 0 conflicts renamed (.conflict-YYYYMMDD)
```

---

#### `exit` / `quit` / `q`
REPL を終了します。

---

### 4.5 keychain サブコマンド

```
kpot keychain test    # バックエンドの診断情報を表示
kpot keychain forget <file>  # 指定 vault のキャッシュを削除
```

```bash
$ kpot keychain test
backend: linux-secret-tool
available: false
config mode: auto
hint: install libsecret-tools and ensure DBUS_SESSION_BUS_ADDRESS is set
```

---

## 5. ノート名のルール

| ルール | 詳細 |
|-------|------|
| 使用可能文字 | `a-z 0-9 . - _ /` (ASCII のみ) |
| 大文字 | 自動的に小文字に変換 |
| 長さ | 1〜128 文字 |
| `/` | ディレクトリ区切りとして使用可能 |
| 先頭・末尾の `/` | 不可 |
| 連続した `//` | 不可 |

```bash
# 有効な名前
ai/openai
server/prod/db
my-secret_key.v2

# 無効な名前 (エラーになる)
/leading-slash
trailing/slash/
double//slash
UPPERCASE         # → uppercase に自動変換
```

---

## 6. テンプレートとプレースホルダー

新規ノート作成時に適用されるテンプレートをカスタマイズできます。

### 組み込みデフォルトテンプレート

```markdown
# {{name}}

- id:
- url:
- password:
- api_key:

## memo

```

### サポートされるプレースホルダー

| プレースホルダー | 展開内容 |
|----------------|---------|
| `{{name}}` | ノートのフルパス名 (例: `ai/openai`) |
| `{{basename}}` | 最終セグメント (例: `openai`) |
| `{{date}}` | 今日の日付 (例: `2026-04-27`) |
| `{{time}}` | 現在時刻 (例: `14:30:00`) |
| `{{datetime}}` | 日時 (例: `2026-04-27 14:30:00`) |

### テンプレートのカスタマイズ

```
kpot:personal> template
```

`$EDITOR` が開き、テンプレートを編集できます。保存すると vault に保存されます。

---

## 7. 設定ファイル

`~/.config/kpot/config.toml` に設定を記述できます。

```toml
# 使用するエディタ ($EDITOR より優先される)
editor = "vim"

# クリップボード自動消去までの秒数 (デフォルト: 30)
clipboard_clear_seconds = 60

# OS キーチェーン動作 ("auto" | "always" | "never", デフォルト: "auto")
keychain = "auto"

# アイドルロックまでの分数 (デフォルト: 10)
idle_lock_minutes = 15
```

---

## 8. 環境変数

| 変数名 | 説明 |
|--------|------|
| `KPOT_PASSPHRASE` | パスフレーズを直接指定 (非推奨・CI 用途のみ) |
| `KPOT_BUNDLE_PASSPHRASE` | bundle/import-bundle のパスフレーズを指定 |
| `EDITOR` | ノート編集に使用するエディタ |
| `VISUAL` | `EDITOR` のフォールバック |

> ⚠️ `KPOT_PASSPHRASE` と `KPOT_BUNDLE_PASSPHRASE` は**意図的に分離**されています。  
> vault 開錠用のパスフレーズがバンドルパスフレーズに混入するのを防ぐためです。

---

## 9. リカバリーキー

v0.3 以降の vault はすべてリカバリーキーを持ちます。  
パスフレーズを忘れた場合でも、リカバリーキーがあれば vault を開けます。

### 種類

#### BIP-39 シードフレーズ (デフォルト)

```
 1. abandon      2. ability      3. able         4. about
 5. above        6. absent       7. absorb        8. abstract
 9. absurd      10. abuse       11. access       12. accident
```

- 12語 (128bit) または 24語 (256bit) のニーモニック
- 業界標準の BIP-39 形式
- オフラインで保管 (金庫・紙など)

#### Crockford Base32 秘密鍵

```
AAAAAAAA-BBBBBBBB-CCCCCCCC-DDDDDDDD-EEEEEEEE-FFFFFFF0-GGGGGGGG
```

- 32バイト (256bit) のランダムシークレット
- O→0, I→1, L→1 の誤字自動修正 (Crockford 仕様)
- 8文字区切りで読み上げ・手書きしやすい

### リカバリーキーで vault を開く

```bash
kpot personal.kpot --recover
Enter recovery seed (or secret key): abandon ability able ...
```

### セキュリティ特性

- リカバリーキーは **init 時に一度だけ `/dev/tty` に表示**され、ログに残りません
- パスフレーズを変更 (`passphrase`) してもリカバリーキーは**引き続き有効**です
- DEK (Data Encryption Key) が分離されているため、パスフレーズ変更時に DEK は変わりません

---

## 10. OS キーチェーン連携

一度パスフレーズを入力すると、OS のネイティブシークレットストアに vault の開錠キーをキャッシュします。  
次回以降はパスフレーズ入力・Argon2id 計算をスキップして高速に開けます。

### 対応バックエンド

| OS | バックエンド | 必要条件 |
|----|------------|---------|
| macOS | Apple Security Services | 標準搭載 |
| Linux | GNOME Secret Service (secret-tool) | `libsecret-tools` + D-Bus セッション |
| Windows | Windows Credential Manager | 標準搭載 |

### Linux でのセットアップ

```bash
# Ubuntu / Debian
sudo apt install libsecret-tools

# Fedora / RHEL
sudo dnf install libsecret

# Arch
sudo pacman -S libsecret
```

### キャッシュ管理

```bash
kpot keychain test                    # 動作確認
kpot personal.kpot --forget           # キャッシュ削除
kpot personal.kpot --no-cache         # キャッシュを使わずに開く
```

### 設定

```toml
# config.toml
keychain = "auto"    # 初回だけ確認 (デフォルト)
keychain = "always"  # 常にキャッシュ
keychain = "never"   # キャッシュしない
```

---

## 11. アイドルロック

REPL セッションで一定時間操作がないと自動的にセッションを終了します。

- **デフォルト**: 10 分
- タイムアウト時にキーマテリアルを消去して終了します
- コマンドを実行するたびにタイマーがリセットされます
- 非 TTY 環境 (パイプ等) では発動しません

### 設定

```toml
# config.toml
idle_lock_minutes = 5   # 5分でロック
idle_lock_minutes = 0   # デフォルト (10分)
```

---

## 12. バンドル転送 (.kpb)

選択したノートだけを暗号化した `.kpb` (kpot bundle) ファイルに書き出し、別の vault へ安全に転送できます。

### 用途

- チームメンバーへの認証情報共有
- 別の vault への選択的なコピー
- バックアップの一部を切り出して共有

### 仕組み

```
bundle passphrase
    ↓ Argon2id
    KEK (Key Encryption Key)
    ↓ XChaCha20-Poly1305
    BEK (Bundle Encryption Key)  ← wrapped_bek フィールドに格納
    ↓ XChaCha20-Poly1305
    Payload (notes JSON)
```

### バンドル作成

```bash
# REPL から
kpot:personal> bundle ai/openai server/fw0 -o transfer.kpb

# シングルショット
kpot personal.kpot bundle ai/openai server/fw0 -o transfer.kpb
```

### バンドルのインポート

```bash
# REPL から
kpot:work> import-bundle transfer.kpb

# シングルショット
kpot work.kpot import-bundle transfer.kpb -y
```

### 注意

- バンドルパスフレーズは受信者に別チャネルで伝えてください
- `KPOT_BUNDLE_PASSPHRASE` 環境変数でバイパス可能 (CI 用途)
- 同名ノートは `.conflict-YYYYMMDD` にリネームされ、**上書きされません**

---

## 13. ファイルフォーマット

### vault ファイル (.kpot)

```json
{
  "format": "kpot",
  "version": 2,
  "passphrase_wrap": {
    "kind": "passphrase",
    "kdf": {
      "name": "argon2id",
      "salt": "<base64>",
      "params": { "memory_kib": 65536, "iterations": 3, "parallelism": 1 }
    },
    "nonce": "<base64 24B>",
    "wrapped_dek": "<base64>"
  },
  "recovery_wrap": {
    "kind": "seed-bip39",
    "kdf": { "name": "pbkdf2-sha512", "iterations": 2048 },
    "nonce": "<base64 24B>",
    "wrapped_dek": "<base64>"
  },
  "cipher": {
    "name": "xchacha20-poly1305",
    "nonce": "<base64 24B>"
  },
  "payload": "<base64 ciphertext + 16B Poly1305 tag>"
}
```

### v1 vault (旧形式・後方互換)

v0.1/v0.2 で作成した vault はそのまま使えます。  
DEK は KDF 鍵と同一です (リカバリーキーなし)。

### プレーンテキスト (復号後)

```json
{
  "version": 1,
  "created_at": "2026-04-27T00:00:00Z",
  "updated_at": "2026-04-27T12:00:00Z",
  "template": "",
  "notes": {
    "ai/openai": {
      "body": "OPENAI_API_KEY=sk-...",
      "created_at": "2026-04-27T00:00:00Z",
      "updated_at": "2026-04-27T12:00:00Z"
    }
  }
}
```

### バンドルファイル (.kpb)

```json
{
  "format": "kpot-bundle",
  "version": 1,
  "kdf": { "name": "argon2id", "salt": "<base64>", "params": { ... } },
  "wrap_nonce": "<base64>",
  "wrapped_bek": "<base64>",
  "cipher": { "name": "xchacha20-poly1305" },
  "nonce": "<base64>",
  "payload": "<base64>"
}
```

---

## 14. セキュリティ設計

### 暗号プリミティブ

| 用途 | アルゴリズム | パラメータ |
|------|------------|----------|
| パスフレーズ → KEK | Argon2id | m=64MiB, t=3, p=1 |
| シードフレーズ → KEK | PBKDF2-HMAC-SHA512 | 2048回 |
| 秘密鍵 → KEK | HKDF-SHA256 | — |
| DEK 暗号化 (wrap) | XChaCha20-Poly1305 | 24B nonce |
| ペイロード暗号化 | XChaCha20-Poly1305 | 24B nonce |
| ナンス生成 | crypto/rand | — |

### AAD (追加認証データ)

ヘッダー全体 (KDF パラメータ・ nonce を含む) が AAD として使われます。  
KDF パラメータのダウングレード・ヘッダーの差し替えは AEAD の認証タグ検証で検出されます。

### キーマテリアルの保護

- 一時ファイルは `/dev/shm` (Linux) または OS 標準テンポラリに配置
- セッション終了時に `crypto.Zero()` でメモリを上書き
- パスフレーズ入力時は `term.ReadPassword` でエコーを抑制
- リカバリーキー表示は `/dev/tty` (Unix) / `os.Stdout` (Windows) に直接書き込み

### 耐改ざん性

```
改ざん箇所      → 検出方法
----------------------------------------------------
KDF params      → Validate() 最小値チェック + AAD 不一致
payload         → Poly1305 タグ検証失敗 (ErrAuthFailed)
nonce           → AAD 不一致 → Poly1305 タグ検証失敗
wrapped_dek     → AEAD 認証失敗 (ErrAuthFailed)
recovery_wrap   → WrapAAD 不一致 → AEAD 認証失敗
```

### 原子書き込み

```
1. <file>.tmp に書き込み + fsync
2. <file> → <file>.bak にリネーム
3. <file>.tmp → <file> にリネーム
4. ディレクトリを fsync
```

クラッシュ後も `<file>` または `<file>.bak` のどちらかは必ず残り、復元できます。

### 既知の制限

| 項目 | 詳細 |
|------|------|
| `crypto.Zero` | GC による移動後のメモリは保証できない (best-effort) |
| keychain | ヘッドレス Linux (D-Bus なし) では無効 |
| リカバリー表示 | TTY 必須 (パイプ・リダイレクト不可) |
| `KPOT_PASSPHRASE` | セットしたまま使うと passphrase コマンドが同じパスフレーズで上書きされる |

---

## 15. 実証ログ

以下は v0.5.0 をサンドボックス環境で実際に実行した結果です。

### テスト環境

```
OS: Linux (ubuntu)
Go: 1.22.3
kpot: 0.5.0-dev
```

### テスト結果サマリー

| # | テスト内容 | 結果 |
|---|-----------|------|
| T01 | 全テストスイート (12パッケージ) | ✅ PASS |
| T02 | ビルド成功 | ✅ PASS |
| T03 | ls (one-shot) | ✅ PASS |
| T04 | read (one-shot) | ✅ PASS |
| T05 | find (one-shot) | ✅ PASS |
| T06 | bundle 作成 (.kpb, 0600) | ✅ PASS |
| T07 | import-bundle (conflict rename) | ✅ PASS |
| T08 | export to JSON | ✅ PASS |
| T09 | rm -y (one-shot) | ✅ PASS |
| T10 | template show | ✅ PASS |
| T11 | 平文ディスク非漏洩 (vault) | ✅ PASS |
| T12 | 平文ディスク非漏洩 (bundle) | ✅ PASS |
| T13 | KDF params 改ざん検出 | ✅ PASS (exit 1) |
| T14 | nonce 改ざん検出 | ✅ PASS (exit 3) |
| T15 | payload 改ざん検出 | ✅ PASS (exit 3) |
| T16 | 誤パスフレーズ拒否 | ✅ PASS (exit 3) |
| T17 | passphrase rotation | ✅ PASS |
| T18 | ファイルパーミッション 0600 | ✅ PASS |
| T19 | recovery-info (v1 vault) | ✅ PASS |
| T20 | keychain test (unavailable) | ✅ PASS |
| T21 | export to file + 0600 | ✅ PASS |
| T22 | import JSON (merge) | ✅ PASS |
| T23 | version フラグ | ✅ PASS |
| T24 | 不正ノート名の拒否 | ✅ PASS |
| T25 | 存在しないファイルのエラー | ✅ PASS |

### 代表的な実行例

#### ls / read / find

```bash
$ KPOT_PASSPHRASE=demopass kpot demo.kpot ls
ai/anthropic
ai/openai
server/fw0

$ KPOT_PASSPHRASE=demopass kpot demo.kpot read ai/openai
OPENAI_API_KEY=sk-demo-1234567890abcdef

## memo
OpenAI API key for demo project.

$ KPOT_PASSPHRASE=demopass kpot demo.kpot find ssh
server/fw0   (body)  Key:  ~/.ssh/id_ed25519_prod
```

#### bundle / import-bundle

```bash
# バンドル作成
kpot:demo> bundle ai/openai server/fw0 -o transfer.kpb
Bundle passphrase (recipient will need it):
wrote 2 notes to transfer.kpb
note: share the passphrase via a separate channel

# バンドルファイル確認
$ cat transfer.kpb | python3 -c "import sys,json; d=json.load(sys.stdin); print(d['format'], 'v'+str(d['version']))"
kpot-bundle v1

# インポート先 vault に取り込み
kpot:work> import-bundle transfer.kpb
Source bundle passphrase:
bundle contains 2 notes:
  ai/openai   OPENAI_API_KEY=sk-...
  server/fw0  Host: prod-fw0.example.com
import 2 notes into this vault? [y/N]: y
imported: +2 new, 0 conflicts renamed (.conflict-YYYYMMDD)
```

#### 改ざん検出

```bash
# KDF memory_kib を改ざん
$ KPOT_PASSPHRASE=demopass kpot tampered.kpot ls
argon2id memory too low: 1000 KiB
# exit code: 1

# payload バイトを反転
$ KPOT_PASSPHRASE=demopass kpot tampered.kpot ls
Wrong passphrase, or the file is corrupted
# exit code: 3
```

---

*このマニュアルは kpot v0.5.0 に基づいています。*  
*最新情報は https://github.com/Shin-R2un/kpot を参照してください。*
