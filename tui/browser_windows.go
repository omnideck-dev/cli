//go:build windows

package tui

import "golang.org/x/sys/windows"

// openBrowser asks the Windows shell to open the URL with the user's default
// browser. Calling the native API avoids launching rundll32 or a command shell.
func openBrowser(url string) error {
	verb, err := windows.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	target, err := windows.UTF16PtrFromString(url)
	if err != nil {
		return err
	}
	return windows.ShellExecute(0, verb, target, nil, nil, windows.SW_SHOWNORMAL)
}
