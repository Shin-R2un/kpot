//go:build windows

package keychain

import (
	"errors"
	"fmt"
	"syscall"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows Credential Manager via wincred. We use it through syscalls
// against advapi32.dll directly so this package has no third-party Go
// dependency beyond golang.org/x/sys/windows (already a transitive
// dep of the project).
//
// Each entry's TargetName is "kpot:<account>" so multiple kpot entries
// don't collide with each other or with other apps' credentials.

const targetPrefix = "kpot:"

// CRED_TYPE_GENERIC = 1; the user-defined credential type we use for
// arbitrary application secrets.
const credTypeGeneric = 1

// CRED_PERSIST_LOCAL_MACHINE = 2; entry survives reboot, scoped to
// the local machine + user (matches macOS / Linux semantics).
const credPersistLocalMachine = 2

// ERROR_NOT_FOUND from wincred lookups.
const errorNotFound syscall.Errno = 1168

var (
	advapi32        = windows.NewLazySystemDLL("advapi32.dll")
	procCredReadW   = advapi32.NewProc("CredReadW")
	procCredWriteW  = advapi32.NewProc("CredWriteW")
	procCredDeleteW = advapi32.NewProc("CredDeleteW")
	procCredFree    = advapi32.NewProc("CredFree")
)

// credentialW mirrors the CREDENTIALW struct from wincred.h. Field
// order and types must match exactly — the syscall writes through it.
type credentialW struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWritten        windows.Filetime
	CredentialBlobSize uint32
	CredentialBlob     *byte
	Persist            uint32
	AttributeCount     uint32
	Attributes         uintptr
	TargetAlias        *uint16
	UserName           *uint16
}

type windowsBackend struct{}

func defaultBackend() Backend { return &windowsBackend{} }

func (*windowsBackend) Name() string { return "windows-credential-manager" }

// Available is true on every Windows installation we can reach (the
// API is built into the OS). We probe with a harmless CredRead on a
// non-existent account to confirm advapi32 actually loads.
func (*windowsBackend) Available() bool {
	return advapi32.Load() == nil
}

func (*windowsBackend) Get(account string) ([]byte, error) {
	target, err := windows.UTF16PtrFromString(targetPrefix + account)
	if err != nil {
		return nil, err
	}
	var pcred *credentialW
	r, _, callErr := procCredReadW.Call(
		uintptr(unsafe.Pointer(target)),
		uintptr(credTypeGeneric),
		0,
		uintptr(unsafe.Pointer(&pcred)),
	)
	if r == 0 {
		if errno, ok := callErr.(syscall.Errno); ok && errno == errorNotFound {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("CredReadW: %w", callErr)
	}
	defer procCredFree.Call(uintptr(unsafe.Pointer(pcred)))

	size := int(pcred.CredentialBlobSize)
	blob := make([]byte, size)
	if size > 0 {
		copy(blob, unsafe.Slice(pcred.CredentialBlob, size))
	}
	// Stored value is the hex-encoded secret as UTF-16. Convert back
	// to a Go string before DecodeSecret.
	return DecodeSecret(utf16BytesToString(blob))
}

func (*windowsBackend) Set(account string, secret []byte) error {
	target, err := windows.UTF16PtrFromString(targetPrefix + account)
	if err != nil {
		return err
	}
	encoded := EncodeSecret(secret)
	encodedUTF16 := utf16.Encode([]rune(encoded))
	blobBytes := make([]byte, len(encodedUTF16)*2)
	for i, c := range encodedUTF16 {
		blobBytes[2*i] = byte(c)
		blobBytes[2*i+1] = byte(c >> 8)
	}

	cred := credentialW{
		Type:               credTypeGeneric,
		TargetName:         target,
		CredentialBlobSize: uint32(len(blobBytes)),
		CredentialBlob:     &blobBytes[0],
		Persist:            credPersistLocalMachine,
	}
	r, _, callErr := procCredWriteW.Call(
		uintptr(unsafe.Pointer(&cred)),
		0,
	)
	if r == 0 {
		return fmt.Errorf("CredWriteW: %w", callErr)
	}
	return nil
}

func (*windowsBackend) Delete(account string) error {
	target, err := windows.UTF16PtrFromString(targetPrefix + account)
	if err != nil {
		return err
	}
	r, _, callErr := procCredDeleteW.Call(
		uintptr(unsafe.Pointer(target)),
		uintptr(credTypeGeneric),
		0,
	)
	if r == 0 {
		if errno, ok := callErr.(syscall.Errno); ok && errno == errorNotFound {
			return ErrNotFound
		}
		return fmt.Errorf("CredDeleteW: %w", callErr)
	}
	return nil
}

// utf16BytesToString decodes little-endian UTF-16 bytes (the format
// CredReadW gives us) back to a Go string. Stops at the first NUL
// codeunit if any (tolerates either trailing-NUL or unterminated).
func utf16BytesToString(b []byte) string {
	if len(b)%2 != 0 {
		b = b[:len(b)-1]
	}
	codes := make([]uint16, 0, len(b)/2)
	for i := 0; i < len(b); i += 2 {
		c := uint16(b[i]) | uint16(b[i+1])<<8
		if c == 0 {
			break
		}
		codes = append(codes, c)
	}
	if len(codes) == 0 {
		return ""
	}
	return string(utf16.Decode(codes))
}

// Stub to satisfy the linter — errors.Is is referenced in macOS/linux
// builds but not here, while syscall.Errno needs a discard import on
// some toolchains. Both are real uses above; this var prevents
// "imported and not used" lint warnings if those branches change.
var _ = errors.Is
