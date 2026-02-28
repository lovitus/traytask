//go:build windows

package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

const (
	msgBoxOK           = 0x00000000
	msgBoxYesNo        = 0x00000004
	msgBoxIconInfo     = 0x00000040
	msgBoxIconQuestion = 0x00000020
	msgBoxDefaultBtn2  = 0x00000100
	msgBoxResultYes    = 6

	envSkipInstallBootstrap = "TRAYTASK_SKIP_INSTALL_CHECK"
)

func ensureInstalledAndRelaunch() (bool, error) {
	if os.Getenv(envSkipInstallBootstrap) == "1" {
		return false, nil
	}

	exe, err := os.Executable()
	if err != nil {
		return false, err
	}
	exe, _ = filepath.EvalSymlinks(exe)
	exe = filepath.Clean(exe)

	installDir, err := trayTaskInstallDir()
	if err != nil {
		return false, err
	}
	targetExe := filepath.Join(installDir, filepath.Base(exe))

	if sameDir(filepath.Dir(exe), installDir) {
		if err := writeDefenderWhitelistBat(installDir); err != nil {
			return false, err
		}
		return false, nil
	}

	targetExists := fileExists(targetExe)
	installedVersion := ""
	if targetExists {
		v, err := queryBinaryVersion(targetExe)
		if err == nil {
			installedVersion = v
			if v == version {
				if err := relaunchFromInstalledExe(targetExe); err != nil {
					return true, err
				}
				return true, nil
			}
		}
	}

	action := "安装"
	if targetExists {
		action = "更新安装"
	}
	prompt := "检测到当前运行目录不是安装目录。\n将" + action + "到：\n" + installDir
	if targetExists {
		if installedVersion == "" {
			installedVersion = "未知"
		}
		prompt += "\n\n已安装版本：" + installedVersion + "\n当前版本：" + version
	}
	prompt += "\n\n是否继续？"
	if !askYesNo(prompt, "TrayTask") {
		showInfo("你已取消安装，程序将退出。", "TrayTask")
		return true, nil
	}

	if err := os.MkdirAll(installDir, 0o755); err != nil {
		return true, err
	}
	if err := copyFile(exe, targetExe); err != nil {
		return true, err
	}
	if err := writeDefenderWhitelistBat(installDir); err != nil {
		return true, err
	}
	_ = openPath(installDir)

	if err := relaunchFromInstalledExe(targetExe); err != nil {
		return true, err
	}

	showInfo(
		"已从安装目录启动。\n\n请右键以管理员身份运行：\n"+filepath.Join(installDir, "add_defender_whitelist.bat")+"\n\n目录已自动打开；若使用第三方安全软件，也请手动加白该目录。",
		"TrayTask 安装完成",
	)
	return true, nil
}

func trayTaskInstallDir() (string, error) {
	base := os.Getenv("LOCALAPPDATA")
	if strings.TrimSpace(base) == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", errors.New("cannot resolve LOCALAPPDATA")
		}
		base = filepath.Join(home, "AppData", "Local")
	}
	return filepath.Join(base, "TrayTask"), nil
}

func queryBinaryVersion(exePath string) (string, error) {
	cmd := exec.Command(exePath, "-internal-print-version")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windowsCreateNoWindow}
	cmd.Env = append(os.Environ(), envSkipInstallBootstrap+"=1")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(bytes.TrimSpace(out))), nil
}

func relaunchFromInstalledExe(targetExe string) error {
	cmd := exec.Command(targetExe, os.Args[1:]...)
	cmd.Dir = filepath.Dir(targetExe)
	if !isCLIExecutableName(filepath.Base(targetExe)) {
		cmd.SysProcAttr = &syscall.SysProcAttr{
			HideWindow:    true,
			CreationFlags: windowsCreateNoWindow,
		}
	}
	return cmd.Start()
}

func fileExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir()
}

func isCLIExecutableName(name string) bool {
	lower := strings.ToLower(name)
	return strings.Contains(lower, "-cli") || strings.Contains(lower, "_cli")
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	tmp := dst + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

func writeDefenderWhitelistBat(installDir string) error {
	bat := `@echo off
setlocal

net session >nul 2>&1
if %errorlevel% neq 0 (
  echo Please right-click and run this script as Administrator.
  echo.
  echo Path: %~f0
  pause
  exit /b 1
)

set INSTALL_DIR=%~dp0
set INSTALL_DIR_NO_TAIL=%INSTALL_DIR:~0,-1%

echo Adding Defender exclusion path: %INSTALL_DIR_NO_TAIL%
powershell -NoProfile -ExecutionPolicy Bypass -Command "Add-MpPreference -ExclusionPath '%INSTALL_DIR_NO_TAIL%'"
if %errorlevel% neq 0 (
  echo Failed to add Defender exclusion path.
  pause
  exit /b 1
)

echo Done.
echo If you use third-party antivirus, add this folder manually to whitelist:
echo %INSTALL_DIR_NO_TAIL%
pause
`
	path := filepath.Join(installDir, "add_defender_whitelist.bat")
	return os.WriteFile(path, []byte(bat), 0o644)
}

func sameDir(a, b string) bool {
	cleanA := strings.TrimRight(filepath.Clean(a), `\\/`)
	cleanB := strings.TrimRight(filepath.Clean(b), `\\/`)
	return strings.EqualFold(cleanA, cleanB)
}

func askYesNo(text, title string) bool {
	ret, _ := messageBox(text, title, msgBoxYesNo|msgBoxIconQuestion|msgBoxDefaultBtn2)
	return ret == msgBoxResultYes
}

func showInfo(text, title string) {
	_, _ = messageBox(text, title, msgBoxOK|msgBoxIconInfo)
}

func messageBox(text, title string, flags uintptr) (int, error) {
	user32 := syscall.NewLazyDLL("user32.dll")
	proc := user32.NewProc("MessageBoxW")
	ret, _, err := proc.Call(
		0,
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(text))),
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(title))),
		flags,
	)
	if ret == 0 && err != syscall.Errno(0) {
		return 0, fmt.Errorf("message box: %w", err)
	}
	return int(ret), nil
}
