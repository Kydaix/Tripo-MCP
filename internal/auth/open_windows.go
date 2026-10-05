//go:build windows

package auth

import "golang.org/x/sys/windows"

// OpenPage asks Windows to use the user's default browser without a debugging connection.
func OpenPage(url string) error {
	p, err := windows.UTF16PtrFromString(url)
	if err != nil {
		return err
	}
	return windows.ShellExecute(0, nil, p, nil, nil, windows.SW_SHOWNORMAL)
}
