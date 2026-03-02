package main

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Server struct {
	manager  *Manager
	tmpl     *template.Template
	apiToken string
}

func NewServer(manager *Manager, apiToken string) (*Server, error) {
	tmpl, err := template.New("index").Parse(indexHTML)
	if err != nil {
		return nil, err
	}
	return &Server{manager: manager, tmpl: tmpl, apiToken: apiToken}, nil
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.Handle("/api/state", s.withAPIAuth(http.HandlerFunc(s.handleState)))
	mux.Handle("/api/env", s.withAPIAuth(http.HandlerFunc(s.handleEnv)))
	mux.Handle("/api/tasks", s.withAPIAuth(http.HandlerFunc(s.handleTasks)))
	mux.Handle("/api/tasks/", s.withAPIAuth(http.HandlerFunc(s.handleTaskAction)))
	mux.Handle("/api/events", s.withAPIAuth(http.HandlerFunc(s.handleEvents)))
	return mux
}

func (s *Server) withAPIAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.isAuthorized(r) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) isAuthorized(r *http.Request) bool {
	token := r.Header.Get("X-TrayTask-Token")
	if token == "" {
		token = r.URL.Query().Get("token")
	}
	return subtle.ConstantTimeCompare([]byte(token), []byte(s.apiToken)) == 1
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	_ = s.tmpl.Execute(w, map[string]string{
		"APIToken": s.apiToken,
	})
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	resp := map[string]any{
		"tasks":     s.manager.ListTasks(),
		"globalEnv": s.manager.GetGlobalEnv(),
	}
	sendJSON(w, http.StatusOK, resp)
}

func (s *Server) handleEnv(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var payload struct {
		Env map[string]string `json:"env"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if err := s.manager.UpdateGlobalEnv(payload.Env); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	sendJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var payload taskPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		task := payload.toTask()
		created, err := s.manager.UpsertTask(task, true)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		sendJSON(w, http.StatusCreated, created)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleTaskAction(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/tasks/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		http.Error(w, "task id required", http.StatusBadRequest)
		return
	}
	id := parts[0]
	if len(parts) == 1 {
		if r.Method == http.MethodPut {
			var payload taskPayload
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, "invalid json", http.StatusBadRequest)
				return
			}
			task := payload.toTask()
			task.ID = id
			updated, err := s.manager.UpsertTask(task, false)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			sendJSON(w, http.StatusOK, updated)
			return
		}
		if r.Method == http.MethodDelete {
			if err := s.manager.DeleteTask(id); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			sendJSON(w, http.StatusOK, map[string]any{"ok": true})
			return
		}
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	action := parts[1]
	switch action {
	case "toggle":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var payload struct {
			Enabled bool `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		if err := s.manager.ToggleTask(id, payload.Enabled); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		sendJSON(w, http.StatusOK, map[string]any{"ok": true})
	case "run":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := s.manager.StartTask(id); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		sendJSON(w, http.StatusOK, map[string]any{"ok": true})
	case "stop":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := s.manager.StopTask(id); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		sendJSON(w, http.StatusOK, map[string]any{"ok": true})
	case "logs":
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		offset := int64(0)
		if raw := r.URL.Query().Get("offset"); raw != "" {
			v, err := strconv.ParseInt(raw, 10, 64)
			if err != nil {
				http.Error(w, "invalid offset", http.StatusBadRequest)
				return
			}
			offset = v
		}
		text, newOffset, eof, err := s.manager.ReadTaskLogs(id, offset, 256*1024)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		sendJSON(w, http.StatusOK, map[string]any{
			"text":      text,
			"newOffset": newOffset,
			"eof":       eof,
		})
	case "clearlogs":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := s.manager.ClearTaskLogs(id); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		sendJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		http.Error(w, fmt.Sprintf("unknown action: %s", action), http.StatusNotFound)
	}
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "stream unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	writeEvent := func(data string) error {
		if _, err := io.WriteString(w, "data: "+data+"\n\n"); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}
	writePing := func() error {
		if _, err := io.WriteString(w, ": ping\n\n"); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}

	if err := writeEvent("ready"); err != nil {
		return
	}
	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-s.manager.NotifyChannel():
			if err := writeEvent("update"); err != nil {
				return
			}
		case <-heartbeat.C:
			if err := writePing(); err != nil {
				return
			}
		}
	}
}

type taskPayload struct {
	Name              string            `json:"name"`
	Command           string            `json:"command"`
	Type              TaskType          `json:"type"`
	Enabled           bool              `json:"enabled"`
	CronExpr          string            `json:"cronExpr"`
	KillPreviousOnRun bool              `json:"killPreviousOnRun"`
	Env               map[string]string `json:"env"`
}

func (p taskPayload) toTask() Task {
	return Task{
		Name:              p.Name,
		Command:           p.Command,
		Type:              p.Type,
		Enabled:           p.Enabled,
		CronExpr:          p.CronExpr,
		KillPreviousOnRun: p.KillPreviousOnRun,
		Env:               p.Env,
	}
}

func sendJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func envToText(env map[string]string) string {
	if len(env) == 0 {
		return ""
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sortStrings(keys)
	lines := make([]string, 0, len(keys))
	for _, k := range keys {
		lines = append(lines, k+"="+env[k])
	}
	return strings.Join(lines, "\n")
}

func parseEnvText(raw string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		k := strings.TrimSpace(parts[0])
		if k == "" {
			continue
		}
		out[k] = strings.TrimSpace(parts[1])
	}
	return out
}

func sortStrings(items []string) {
	if len(items) < 2 {
		return
	}
	for i := 0; i < len(items)-1; i++ {
		for j := i + 1; j < len(items); j++ {
			if items[j] < items[i] {
				items[i], items[j] = items[j], items[i]
			}
		}
	}
}

const indexHTML = `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>TrayTask 管理台</title>
  <style>
    :root {
      --bg: #f4f7f1;
      --card: #ffffff;
      --ink: #152018;
      --muted: #5d6c61;
      --line: #d5dfd2;
      --green: #14813a;
      --amber: #ab6a00;
      --red: #a12525;
      --blue: #145ea8;
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      font-family: "Noto Sans SC", "PingFang SC", "Microsoft YaHei", sans-serif;
      color: var(--ink);
      background:
        radial-gradient(1200px 500px at 10% -10%, #d9ead7 0%, transparent 70%),
        radial-gradient(1000px 460px at 90% -20%, #e7efd8 0%, transparent 70%),
        var(--bg);
      min-height: 100vh;
    }
    .wrap {
      max-width: 1300px;
      margin: 0 auto;
      padding: 20px;
      display: grid;
      gap: 16px;
      grid-template-columns: 360px 1fr;
    }
    .card {
      background: var(--card);
      border: 1px solid var(--line);
      border-radius: 14px;
      padding: 14px;
      box-shadow: 0 10px 24px rgba(13, 36, 18, 0.05);
    }
    h1 { margin: 0 0 12px; font-size: 20px; }
    h2 { margin: 0 0 10px; font-size: 16px; }
    label { display: block; font-size: 12px; color: var(--muted); margin: 8px 0 4px; }
    input, select, textarea, button {
      width: 100%;
      border: 1px solid var(--line);
      border-radius: 10px;
      padding: 8px 10px;
      font-size: 14px;
      background: #fff;
      color: var(--ink);
    }
    textarea { min-height: 90px; resize: vertical; font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
    button {
      background: linear-gradient(180deg, #1b7a43, #186639);
      color: #fff;
      border: 0;
      cursor: pointer;
      transition: transform .12s ease, opacity .12s ease;
    }
    button:hover { transform: translateY(-1px); }
    button.alt { background: #f3f7f2; color: var(--ink); border: 1px solid var(--line); }
    .row { display: grid; grid-template-columns: 1fr 1fr; gap: 8px; }
    .inline { display: flex; align-items: center; gap: 8px; margin-top: 8px; }
    .inline input { width: auto; }
    .main { display: grid; gap: 16px; }
    table { width: 100%; border-collapse: collapse; font-size: 13px; }
    th, td { text-align: left; border-bottom: 1px solid var(--line); padding: 8px; vertical-align: top; }
    th { color: var(--muted); font-weight: 600; }
    .status {
      display: inline-flex;
      align-items: center;
      gap: 6px;
      white-space: normal;
    }
    .dot {
      width: 10px;
      height: 10px;
      border-radius: 999px;
      display: inline-block;
    }
    .dot.green { background: var(--green); }
    .dot.amber { background: var(--amber); }
    .dot.red { background: var(--red); }
    .dot.gray { background: #7c8a7f; }
    .badge {
      display: inline-block;
      border-radius: 999px;
      padding: 2px 8px;
      font-size: 12px;
      font-weight: 600;
      white-space: nowrap;
    }
    .badge.on {
      background: #dff4e5;
      color: #0f6a30;
      border: 1px solid #9ed3ad;
    }
    .badge.off {
      background: #eef1ef;
      color: #4f5a52;
      border: 1px solid #c4cec7;
    }
    .actions { display: grid; grid-template-columns: repeat(4, minmax(120px, 1fr)); gap: 6px; max-width: 720px; }
    .actions button { padding: 6px 8px; font-size: 12px; }
    .actions .danger { background: #b53a3a; }
    .task-actions-row td {
      padding-top: 0;
      padding-bottom: 12px;
      border-bottom: 1px solid var(--line);
    }
    .task-main-row td { border-bottom: 0; }
    .actions button:disabled {
      opacity: 0.5;
      cursor: not-allowed;
      transform: none;
    }
    #logPanel { min-height: 240px; font-family: ui-monospace, SFMono-Regular, Menlo, monospace; background: #0f1e14; color: #bdeecb; }
    .muted { color: var(--muted); font-size: 12px; }
    .ok { color: var(--green); }
    .err { color: var(--red); }
    @media (max-width: 980px) {
      .wrap { grid-template-columns: 1fr; }
      .actions { grid-template-columns: repeat(2, minmax(120px, 1fr)); }
    }
  </style>
</head>
<body>
  <div class="wrap">
    <section class="card">
      <h1>TrayTask</h1>
      <p class="muted">托盘任务管理（长期进程 / 一次性命令 / Cron / 实时日志 / 环境变量）</p>

      <h2 id="taskFormTitle">新增任务</h2>
      <input id="taskId" type="hidden" />
      <label>任务名称</label>
      <input id="name" placeholder="例如: 网络连通性检查" />

      <label>命令</label>
      <textarea id="command" placeholder="例如: ping 8.8.8.8 或 curl -I https://example.com"></textarea>

      <div class="row">
        <div>
          <label>任务类型</label>
          <select id="type">
            <option value="long_running">长期执行</option>
            <option value="one_shot">执行完即关闭</option>
          </select>
        </div>
        <div>
          <label>Cron 表达式 (含秒)</label>
          <input id="cronExpr" placeholder="*/30 * * * * *" />
        </div>
      </div>

      <div class="inline">
        <input id="enabled" type="checkbox" checked /> <span>添加后立即启用（绿标）</span>
      </div>
      <div class="inline">
        <input id="killPreviousOnRun" type="checkbox" /> <span>长期任务 Cron 触发时先终止旧任务</span>
      </div>

      <label>任务级环境变量（每行 KEY=VALUE）</label>
      <textarea id="taskEnv" placeholder="HTTP_PROXY=http://127.0.0.1:7890"></textarea>

      <div class="row" style="margin-top:10px;">
        <button id="saveTaskBtn">保存任务</button>
        <button id="resetTaskBtn" class="alt">清空表单</button>
      </div>

      <hr style="border:none; border-top:1px solid var(--line); margin:14px 0;" />

      <h2>全局环境变量</h2>
      <label>全局环境变量（每行 KEY=VALUE）</label>
      <textarea id="globalEnv"></textarea>
      <button id="saveEnvBtn">保存全局环境变量</button>
      <p id="tip" class="muted"></p>
    </section>

    <section class="main">
      <section class="card">
        <h2>任务列表</h2>
        <p class="muted">状态说明：启用/禁用 与 运行状态分开展示；按钮文案会显示“动作(当前状态)”，并按状态自动可用或禁用。</p>
        <table>
          <thead>
            <tr>
              <th>启用</th>
              <th>运行状态</th>
              <th>任务</th>
              <th>命令</th>
              <th>类型</th>
              <th>Cron / 下次</th>
              <th>最近结果</th>
            </tr>
          </thead>
          <tbody id="taskRows"></tbody>
        </table>
      </section>

      <section class="card">
        <h2>日志查看</h2>
        <p class="muted">每个任务日志仅保留最近 1000 行（仅内存，不落盘）。</p>
        <div class="row">
          <div>
            <label>当前任务 ID</label>
            <input id="logTaskId" readonly />
          </div>
          <div>
            <label>日志游标（自动）</label>
            <input id="logOffset" readonly />
          </div>
        </div>
        <textarea id="logPanel" readonly></textarea>
      </section>
    </section>
  </div>

<script>
const API_TOKEN = "{{.APIToken}}";
const state = {
  tasks: [],
  logTaskId: "",
  logOffset: 0,
  polling: null,
  events: null,
};

function envToText(env) {
  return Object.keys(env || {}).sort().map(function(k) { return k + "=" + env[k]; }).join("\n");
}

function parseEnvText(text) {
  const out = {};
  (text || "").split(/\r?\n/).forEach(line => {
    const t = line.trim();
    if (!t || t.startsWith("#")) return;
    const idx = t.indexOf("=");
    if (idx <= 0) return;
    const key = t.slice(0, idx).trim();
    const val = t.slice(idx + 1).trim();
    if (!key) return;
    out[key] = val;
  });
  return out;
}

function esc(v) {
  return String(v || '')
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
    .replaceAll("'", '&#39;');
}

function enabledBadge(item) {
  if (item.task.enabled) return '<span class="badge on">已启用</span>';
  return '<span class="badge off">已禁用</span>';
}

function runtimeInfo(item) {
  if (item.state.running) {
    const pid = item.state.pid ? (' (PID ' + item.state.pid + ')') : '';
    return ["green", "运行中" + pid];
  }
  switch (item.state.status) {
    case "starting": return ["amber", "启动中"];
    case "disabled": return ["gray", "已禁用"];
    case "idle": return ["green", "空闲"];
    case "success": return ["green", "上次成功"];
    case "failed": return ["red", "上次失败"];
    case "stopped": return ["amber", "已停止"];
    case "stopping": return ["amber", "停止中"];
    case "skipped": return ["amber", "已跳过(上次未结束)"];
    case "invalid_cron": return ["red", "Cron 配置错误"];
    default: return ["amber", item.state.status || "未知"];
  }
}

function cronAndNext(item) {
  const cron = item.task.cronExpr ? esc(item.task.cronExpr) : '-';
  const next = fmtTime(item.state.nextRun);
  return cron + '<br><span class="muted">下次: ' + esc(next) + '</span>';
}

function lastResult(item) {
  if (item.state.running) return '运行中...';
  if (!item.state.lastRunEnd) return '-';
  const time = fmtTime(item.state.lastRunEnd);
  const exit = item.state.lastExitCode;
  let header = '完成';
  if (typeof exit === 'number') {
    header = exit === 0 ? '成功 (exit=0)' : ('失败 (exit=' + exit + ')');
  }
  let details = '';
  if (item.state.lastError) {
    details = '<br><span class="muted">' + esc(item.state.lastError) + '</span>';
  }
  return esc(header) + details + '<br><span class="muted">' + esc(time) + '</span>';
}

function fmtTime(raw) {
  if (!raw) return "-";
  try { return new Date(raw).toLocaleString(); } catch { return raw; }
}

function msg(text, ok = true) {
  const tip = document.getElementById("tip");
  tip.className = ok ? "ok" : "err";
  tip.textContent = text;
  setTimeout(() => { tip.textContent = ""; tip.className = "muted"; }, 3200);
}

function toUserError(err) {
  const t = String(err || '');
  if (t.includes('task not found')) return '任务不存在，可能已被删除';
  if (t.includes('task is disabled')) return '任务已禁用，请先启用';
  if (t.includes('task is already running')) return '任务已经在运行中';
  if (t.includes('task is not running')) return '任务当前未运行';
  if (t.includes('cannot stop task process')) return '任务进程当前不可停止';
  if (t.includes('unauthorized')) return '页面授权已失效，请刷新页面';
  return t;
}

async function refreshState() {
  const data = await api('/api/state');
  state.tasks = data.tasks || [];
  document.getElementById("globalEnv").value = envToText(data.globalEnv || {});
  renderTasks();
}

function renderTasks() {
  const tbody = document.getElementById("taskRows");
  tbody.innerHTML = "";
  for (const item of state.tasks) {
    const tr = document.createElement("tr");
    tr.className = "task-main-row";
    const [dot, runtime] = runtimeInfo(item);
    const runDisabled = !item.task.enabled || item.state.running;
    const stopDisabled = !item.state.running;
    const runStateText = item.state.running ? "运行中" : "未运行";
    const runLabel = item.task.type === 'long_running'
      ? ('启动(' + runStateText + ')')
      : ('执行一次(' + runStateText + ')');
    const toggleLabel = item.task.enabled ? '禁用(已启用)' : '启用(已禁用)';
    const stopLabel = item.state.running ? '停止(运行中)' : '停止(未运行)';
    tr.innerHTML =
      "<td>" + enabledBadge(item) + "</td>" +
      "<td><span class=\"status\"><span class=\"dot " + dot + "\"></span>" + esc(runtime) + "</span></td>" +
      "<td>" + esc(item.task.name) + "</td>" +
      "<td><code>" + esc(item.task.command) + "</code></td>" +
      "<td>" + (item.task.type === 'long_running' ? '长期' : '一次性') + "</td>" +
      "<td>" + cronAndNext(item) + "</td>" +
      "<td>" + lastResult(item) + "</td>";
    const actionTr = document.createElement("tr");
    actionTr.className = "task-actions-row";
    actionTr.innerHTML =
      "<td colspan=\"7\"><div class=\"actions\">" +
      "<button data-a=\"edit\" data-id=\"" + item.task.id + "\" class=\"alt\">编辑</button>" +
      "<button data-a=\"toggle\" data-id=\"" + item.task.id + "\" class=\"alt\">" + toggleLabel + "</button>" +
      "<button data-a=\"run\" data-id=\"" + item.task.id + "\" " + (runDisabled ? "disabled" : "") + ">" + runLabel + "</button>" +
      "<button data-a=\"stop\" data-id=\"" + item.task.id + "\" class=\"alt\" " + (stopDisabled ? "disabled" : "") + ">" + stopLabel + "</button>" +
      "<button data-a=\"log\" data-id=\"" + item.task.id + "\" class=\"alt\">日志(查看)</button>" +
      "<button data-a=\"clearlog\" data-id=\"" + item.task.id + "\" class=\"alt\">清空日志(最近1000行)</button>" +
      "<button data-a=\"del\" data-id=\"" + item.task.id + "\" class=\"danger\">删除(任务)</button>" +
      "</div></td>";
    tbody.appendChild(tr);
    tbody.appendChild(actionTr);
  }
}

async function api(path, method='GET', body=null) {
  const opts = { method, headers: { 'X-TrayTask-Token': API_TOKEN }, cache: 'no-store' };
  if (body !== null) {
    opts.headers['Content-Type'] = 'application/json';
    opts.body = JSON.stringify(body);
  }
  const res = await fetch(path, opts);
  if (!res.ok) throw new Error(await res.text());
  if (res.status === 204) return null;
  return res.json();
}

function fillTaskForm(item) {
  document.getElementById("taskFormTitle").textContent = "编辑任务";
  document.getElementById("taskId").value = item.task.id;
  document.getElementById("name").value = item.task.name;
  document.getElementById("command").value = item.task.command;
  document.getElementById("type").value = item.task.type;
  document.getElementById("enabled").checked = !!item.task.enabled;
  document.getElementById("cronExpr").value = item.task.cronExpr || "";
  document.getElementById("killPreviousOnRun").checked = !!item.task.killPreviousOnRun;
  document.getElementById("taskEnv").value = envToText(item.task.env || {});
}

function clearTaskForm() {
  document.getElementById("taskFormTitle").textContent = "新增任务";
  document.getElementById("taskId").value = "";
  document.getElementById("name").value = "";
  document.getElementById("command").value = "";
  document.getElementById("type").value = "long_running";
  document.getElementById("enabled").checked = true;
  document.getElementById("cronExpr").value = "";
  document.getElementById("killPreviousOnRun").checked = false;
  document.getElementById("taskEnv").value = "";
}

async function saveTask() {
  const payload = {
    name: document.getElementById("name").value,
    command: document.getElementById("command").value,
    type: document.getElementById("type").value,
    enabled: document.getElementById("enabled").checked,
    cronExpr: document.getElementById("cronExpr").value,
    killPreviousOnRun: document.getElementById("killPreviousOnRun").checked,
    env: parseEnvText(document.getElementById("taskEnv").value),
  };
  const id = document.getElementById("taskId").value;
  if (id) {
    await api('/api/tasks/' + id, 'PUT', payload);
    msg('任务已更新');
  } else {
    await api('/api/tasks', 'POST', payload);
    msg('任务已创建');
  }
  clearTaskForm();
  await refreshState();
}

async function saveGlobalEnv() {
  const env = parseEnvText(document.getElementById("globalEnv").value);
  await api('/api/env', 'PUT', { env });
  msg('全局环境变量已保存');
  await refreshState();
}

async function onTaskAction(e) {
  const btn = e.target.closest('button[data-a]');
  if (!btn) return;
  if (btn.disabled) return;
  const id = btn.dataset.id;
  const action = btn.dataset.a;
  const item = state.tasks.find(t => t.task.id === id);
  if (!item) return;
  try {
    if (action === 'edit') {
      fillTaskForm(item);
      return;
    }
    if (action === 'toggle') {
      btn.disabled = true;
      await api('/api/tasks/' + id + '/toggle', 'POST', { enabled: !item.task.enabled });
      msg(!item.task.enabled ? '任务已启用（绿标）' : '任务已停用');
    } else if (action === 'run') {
      btn.disabled = true;
      await api('/api/tasks/' + id + '/run', 'POST', {});
      msg('已触发执行');
    } else if (action === 'stop') {
      btn.disabled = true;
      await api('/api/tasks/' + id + '/stop', 'POST', {});
      msg('已发送停止信号');
    } else if (action === 'del') {
      btn.disabled = true;
      if (!confirm('确认删除该任务？')) return;
      await api('/api/tasks/' + id, 'DELETE');
      if (state.logTaskId === id) {
        state.logTaskId = '';
        state.logOffset = 0;
        document.getElementById("logTaskId").value = '';
        document.getElementById("logOffset").value = '0';
        document.getElementById("logPanel").value = '';
      }
      msg('任务已删除');
    } else if (action === 'log') {
      state.logTaskId = id;
      state.logOffset = 0;
      document.getElementById("logTaskId").value = id;
      document.getElementById("logOffset").value = '0';
      document.getElementById("logPanel").value = '';
      await pollLogs();
      msg('日志窗口已切换');
      return;
    } else if (action === 'clearlog') {
      btn.disabled = true;
      if (!confirm('确认清空该任务的日志？')) return;
      await api('/api/tasks/' + id + '/clearlogs', 'POST', {});
      if (state.logTaskId === id) {
        state.logOffset = 0;
        document.getElementById("logOffset").value = '0';
        document.getElementById("logPanel").value = '';
      }
      msg('日志已清空');
    }
    await refreshState();
  } catch (err) {
    msg(toUserError(err), false);
  } finally {
    btn.disabled = false;
    refreshState().catch(() => {});
  }
}

async function pollLogs() {
  if (!state.logTaskId) return;
  try {
    const prevOffset = state.logOffset;
    const data = await api('/api/tasks/' + state.logTaskId + '/logs?offset=' + state.logOffset);
    const panel = document.getElementById("logPanel");
    const nextOffset = Number.isFinite(data.newOffset) ? data.newOffset : state.logOffset;
    const resetPanel = nextOffset < prevOffset;
    if (resetPanel) {
      panel.value = '';
    }
    if (data.text) {
      panel.value += data.text;
    }
    const lines = panel.value.split(/\r?\n/);
    if (lines.length > 1000) {
      panel.value = lines.slice(lines.length - 1000).join('\n');
    }
    panel.scrollTop = panel.scrollHeight;
    state.logOffset = nextOffset;
    document.getElementById("logOffset").value = String(state.logOffset);
  } catch (err) {
    msg(toUserError(err), false);
  }
}

document.getElementById('saveTaskBtn').addEventListener('click', () => saveTask().catch(err => msg(toUserError(err), false)));
document.getElementById('resetTaskBtn').addEventListener('click', clearTaskForm);
document.getElementById('saveEnvBtn').addEventListener('click', () => saveGlobalEnv().catch(err => msg(toUserError(err), false)));
document.getElementById('taskRows').addEventListener('click', onTaskAction);

function connectEvents() {
  if (state.events) {
    try { state.events.close(); } catch {}
  }
  const url = '/api/events?token=' + encodeURIComponent(API_TOKEN);
  const es = new EventSource(url);
  es.onmessage = () => {
    refreshState().catch(() => {});
  };
  es.onerror = () => {
    try { es.close(); } catch {}
    setTimeout(connectEvents, 2000);
  };
  state.events = es;
}

(async function init() {
  try {
    await refreshState();
    connectEvents();
    const params = new URLSearchParams(location.search);
    if (params.get('add') === '1') {
      clearTaskForm();
      document.getElementById('name').focus();
    }
    state.polling = setInterval(async () => {
      await pollLogs();
    }, 1000);
    setInterval(() => {
      refreshState().catch(() => {});
    }, 10000);
  } catch (err) {
    msg(toUserError(err), false);
  }
})();
</script>
</body>
</html>`
