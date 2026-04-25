# `.kpot` file format v1

A `.kpot` file is a single JSON document with an unencrypted header and a
base64‑encoded ciphertext payload.

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
  "payload": "<base64 ciphertext including 16-byte Poly1305 tag>"
}
```

## Encryption

- **KDF**: Argon2id with the parameters in the header. Output 32 bytes.
- **Cipher**: XChaCha20‑Poly1305 (RFC 8439 + XChaCha20 nonce extension).
  Nonce is fresh `crypto/rand` 24 bytes per write.
- **AAD**: the canonical JSON of the header *without* the `payload` field.
  This binds the KDF parameters and cipher choice to the ciphertext, so
  any tampering (KDF downgrade, salt swap, cipher rename) makes
  decryption fail with the standard `ErrAuthFailed`.

## Plaintext payload (after decryption)

```json
{
  "version": 1,
  "created_at": "2026-04-25T12:00:00Z",
  "updated_at": "2026-04-25T12:00:00Z",
  "notes": {
    "ai/openai": {
      "body": "OPENAI_API_KEY=...",
      "created_at": "...",
      "updated_at": "..."
    }
  }
}
```

Note name rules: ASCII `[a-z0-9._/-]`, 1..128 chars, lowercased,
no leading/trailing `/`, no `//`.

## On‑disk lifecycle

- `<file>.tmp` — write target during a save (unlinked on failure)
- `<file>.bak` — previous generation, encrypted with the same passphrase
- `<file>` — current vault

A successful save sequence:

1. write `<file>.tmp`, `fsync`, `close`
2. `rename(<file> → <file>.bak)`  (only if `<file>` exists)
3. `rename(<file>.tmp → <file>)`
4. `fsync(dir)`

A crash at any point leaves either `<file>` or `<file>.bak` (or both)
intact and decryptable.

## Versioning

The top‑level `version` is the **format** version. v2 will be reserved
for additive changes (e.g. seed‑phrase recovery, new cipher option).
The plaintext payload also carries its own `version` field so vault
contents can evolve independently of the envelope.
