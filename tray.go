package main

import (
	"fmt"
	"time"

	"github.com/getlantern/systray"
)

type TrayApp struct {
	manager    *Manager
	dashboard  string
	dataDir    string
	installDir string
}

func NewTrayApp(manager *Manager, dashboard, dataDir, installDir string) *TrayApp {
	return &TrayApp{manager: manager, dashboard: dashboard, dataDir: dataDir, installDir: installDir}
}

func (t *TrayApp) Run() {
	systray.Run(t.onReady, t.onExit)
}

func (t *TrayApp) onReady() {
	systray.SetTitle("TrayTask")
	systray.SetTooltip("TrayTask - 托盘任务管理")
	startExternalShutdownWatcher(func() {
		systray.Quit()
	})
	icon := trayIconBytes()
	if len(icon) > 0 {
		systray.SetIcon(icon)
	}

	var openItem *systray.MenuItem
	var addItem *systray.MenuItem
	if t.dashboard != "" {
		openItem = systray.AddMenuItem("打开管理台", "Open dashboard")
		addItem = systray.AddMenuItem("新增任务", "Add task")
	} else {
		disabled := systray.AddMenuItem("网页管理台已禁用(-web=false)", "Web disabled")
		disabled.Disable()
	}
	openDirItem := systray.AddMenuItem("打开数据目录", "Open TrayTask data directory")
	var openInstallDirItem *systray.MenuItem
	if t.installDir != "" {
		openInstallDirItem = systray.AddMenuItem("打开安装目录(白名单脚本)", "Open install directory containing whitelist script")
	}
	systray.AddSeparator()
	statusItem := systray.AddMenuItem("运行概览: 加载中...", "Task summary")
	statusItem.Disable()
	systray.AddSeparator()
	quitItem := systray.AddMenuItem("退出", "Quit")

	go func() {
		for {
			select {
			case <-openDirItem.ClickedCh:
				_ = openPath(t.dataDir)
			case <-clickedCh(openInstallDirItem):
				if t.installDir != "" {
					_ = openPath(t.installDir)
				}
			case <-clickedCh(openItem):
				if t.dashboard != "" {
					_ = openBrowser(t.dashboard)
				}
			case <-clickedCh(addItem):
				if t.dashboard != "" {
					u := t.dashboard + "?add=1"
					_ = openBrowser(u)
				}
			case <-quitItem.ClickedCh:
				systray.Quit()
				return
			}
		}
	}()

	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			<-ticker.C
			tasks := t.manager.ListTasks()
			total := len(tasks)
			running := 0
			enabled := 0
			failed := 0
			for _, it := range tasks {
				if it.Task.Enabled {
					enabled++
				}
				if it.State.Running {
					running++
				}
				if it.State.Status == "failed" {
					failed++
				}
			}
			statusItem.SetTitle(fmt.Sprintf("运行概览: 总 %d / 启用 %d / 运行中 %d / 失败 %d", total, enabled, running, failed))
		}
	}()
}

func (t *TrayApp) onExit() {
	t.manager.Shutdown()
}

func clickedCh(item *systray.MenuItem) <-chan struct{} {
	if item == nil {
		return nil
	}
	return item.ClickedCh
}
