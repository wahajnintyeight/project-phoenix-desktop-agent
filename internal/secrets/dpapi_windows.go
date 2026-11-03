package secrets

import (
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Entropy binds blobs to this application's use of DPAPI, so a blob from some other
// program's store is not accepted here by accident. It is not a secret.
var entropy = []byte("phoenix.vault.v1")

func blobOf(b []byte) windows.DataBlob {
	if len(b) == 0 {
		return windows.DataBlob{}
	}
	return windows.DataBlob{Size: uint32(len(b)), Data: &b[0]}
}

func protect(plain []byte) ([]byte, error) {
	in, ent := blobOf(plain), blobOf(entropy)
	var out windows.DataBlob
	if err := windows.CryptProtectData(&in, nil, &ent, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return append([]byte(nil), unsafe.Slice(out.Data, out.Size)...), nil
}

func unprotect(blob []byte) ([]byte, error) {
	if len(blob) == 0 {
		return nil, errors.New("empty blob")
	}
	in, ent := blobOf(blob), blobOf(entropy)
	var out windows.DataBlob
	if err := windows.CryptUnprotectData(&in, nil, &ent, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	plain := append([]byte(nil), unsafe.Slice(out.Data, out.Size)...)
	// Wipe the DPAPI-owned copy before it is freed.
	clear(unsafe.Slice(out.Data, out.Size))
	return plain, nil
}
