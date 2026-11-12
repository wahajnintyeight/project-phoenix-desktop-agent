package main

import (
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

func installedVersion() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	var zero windows.Handle
	size, err := windows.GetFileVersionInfoSize(exe, &zero)
	if err != nil {
		return "", err
	}
	if size == 0 {
		return "", fmt.Errorf("executable has no version resource")
	}
	data := make([]byte, size)
	if err := windows.GetFileVersionInfo(exe, 0, size, unsafe.Pointer(&data[0])); err != nil {
		return "", err
	}
	var info *windows.VS_FIXEDFILEINFO
	var infoSize uint32
	if err := windows.VerQueryValue(unsafe.Pointer(&data[0]), `\`, unsafe.Pointer(&info), &infoSize); err != nil {
		return "", err
	}
	if info == nil || infoSize < uint32(unsafe.Sizeof(*info)) || info.Signature != 0xFEEF04BD {
		return "", fmt.Errorf("executable has an invalid version resource")
	}
	return fmt.Sprintf("%d.%d.%d", info.FileVersionMS>>16, info.FileVersionMS&0xffff, info.FileVersionLS>>16), nil
}
