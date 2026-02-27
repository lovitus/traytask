package main

import (
	"fmt"
	"net/url"
	"time"

	"github.com/getlantern/systray"
)

type TrayApp struct {
	manager   *Manager
	dashboard string
}

func NewTrayApp(manager *Manager, dashboard string) *TrayApp {
	return &TrayApp{manager: manager, dashboard: dashboard}
}

func (t *TrayApp) Run() {
	systray.Run(t.onReady, t.onExit)
}

func (t *TrayApp) onReady() {
	systray.SetTitle("TrayTask")
	systray.SetTooltip("TrayTask - 托盘任务管理")

	openItem := systray.AddMenuItem("打开管理台", "Open dashboard")
	addItem := systray.AddMenuItem("新增任务", "Add task")
	systray.AddSeparator()
	statusItem := systray.AddMenuItem("运行概览: 加载中...", "Task summary")
	statusItem.Disable()
	systray.AddSeparator()
	quitItem := systray.AddMenuItem("退出", "Quit")

	go func() {
		for {
			select {
			case <-openItem.ClickedCh:
				_ = openBrowser(t.dashboard)
			case <-addItem.ClickedCh:
				u := t.dashboard + "?add=1"
				_ = openBrowser(u)
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

func withQuery(base string, key string, value string) string {
	u, err := url.Parse(base)
	if err != nil {
		return base
	}
	q := u.Query()
	q.Set(key, value)
	u.RawQuery = q.Encode()
	return u.String()
}
