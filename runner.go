package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

type activeProc struct {
	cmd       *exec.Cmd
	cancel    context.CancelFunc
	runID     string
	taskID    string
	isLongRun bool
}

type taskState struct {
	TaskRuntimeState
}

type Manager struct {
	mu       sync.RWMutex
	logMu    sync.Mutex
	store    *Store
	cfg      AppConfig
	cron     *cron.Cron
	entries  map[string]cron.EntryID
	states   map[string]*taskState
	procs    map[string]*activeProc
	notifyCh chan struct{}
}

const maxLogLinesPerTask = 1000

func NewManager(store *Store) (*Manager, error) {
	cfg, err := store.Load()
	if err != nil {
		return nil, err
	}
	m := &Manager{
		store:    store,
		cfg:      cfg,
		cron:     cron.New(cron.WithSeconds()),
		entries:  map[string]cron.EntryID{},
		states:   map[string]*taskState{},
		procs:    map[string]*activeProc{},
		notifyCh: make(chan struct{}, 1),
	}
	for _, t := range cfg.Tasks {
		m.states[t.ID] = &taskState{TaskRuntimeState: TaskRuntimeState{
			TaskID:  t.ID,
			Enabled: t.Enabled,
			Status:  disabledOrIdle(t.Enabled),
			Running: false,
		}}
	}
	m.cron.Start()
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.reconcileLocked(); err != nil {
		return nil, err
	}
	return m, nil
}

func disabledOrIdle(enabled bool) string {
	if enabled {
		return "idle"
	}
	return "disabled"
}

func (m *Manager) notify() {
	select {
	case m.notifyCh <- struct{}{}:
	default:
	}
}

func (m *Manager) NotifyChannel() <-chan struct{} {
	return m.notifyCh
}

func (m *Manager) Shutdown() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range m.procs {
		if p.cancel != nil {
			p.cancel()
		}
	}
	m.cron.Stop()
}

func (m *Manager) reconcileLocked() error {
	for taskID, entry := range m.entries {
		m.cron.Remove(entry)
		delete(m.entries, taskID)
	}
	for _, t := range m.cfg.Tasks {
		if _, ok := m.states[t.ID]; !ok {
			m.states[t.ID] = &taskState{TaskRuntimeState: TaskRuntimeState{
				TaskID:  t.ID,
				Status:  disabledOrIdle(t.Enabled),
				Enabled: t.Enabled,
			}}
		}
		st := m.states[t.ID]
		st.Enabled = t.Enabled
		if !t.Enabled {
			if p, ok := m.procs[t.ID]; ok && p.cancel != nil {
				p.cancel()
			}
			st.Running = false
			st.Pid = 0
			st.Status = "disabled"
			continue
		}
		if strings.TrimSpace(t.CronExpr) != "" {
			task := t
			entry, err := m.cron.AddFunc(task.CronExpr, func() {
				m.runScheduled(task.ID)
			})
			if err != nil {
				m.logSystem(task.ID, "[scheduler] cron expression invalid: "+err.Error())
				st.Status = "invalid_cron"
				st.LastError = err.Error()
				continue
			}
			m.entries[task.ID] = entry
			next := m.cron.Entry(entry).Next
			if !next.IsZero() {
				n := next
				st.NextRun = &n
			}
			if st.Status == "disabled" || st.Status == "invalid_cron" {
				st.Status = "idle"
			}
		}
		if t.Type == TaskTypeLongRunning && strings.TrimSpace(t.CronExpr) == "" {
			if _, ok := m.procs[t.ID]; !ok {
				go m.startTask(t.ID, "auto-start")
			}
			continue
		}
		if !st.Running && st.Status == "disabled" {
			st.Status = "idle"
		}
	}
	m.notify()
	return nil
}

func (m *Manager) persistLocked() error {
	return m.store.Save(m.cfg)
}

func (m *Manager) findTaskIndexLocked(id string) int {
	for i, t := range m.cfg.Tasks {
		if t.ID == id {
			return i
		}
	}
	return -1
}

func newID() string {
	return strconv.FormatInt(time.Now().UnixNano(), 36)
}

func (m *Manager) ListTasks() []TaskWithState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]TaskWithState, 0, len(m.cfg.Tasks))
	for _, t := range m.cfg.Tasks {
		st := TaskRuntimeState{TaskID: t.ID, Enabled: t.Enabled, Status: disabledOrIdle(t.Enabled)}
		if s, ok := m.states[t.ID]; ok {
			st = s.TaskRuntimeState
		}
		if entry, ok := m.entries[t.ID]; ok {
			n := m.cron.Entry(entry).Next
			if !n.IsZero() {
				st.NextRun = &n
			}
		}
		out = append(out, TaskWithState{Task: t, State: st})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Task.CreatedAt.Before(out[j].Task.CreatedAt)
	})
	return out
}

func (m *Manager) GetGlobalEnv() map[string]string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	cpy := make(map[string]string, len(m.cfg.GlobalEnv))
	for k, v := range m.cfg.GlobalEnv {
		cpy[k] = v
	}
	return cpy
}

func (m *Manager) UpdateGlobalEnv(env map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg.GlobalEnv = normalizeEnvMap(env)
	if err := m.persistLocked(); err != nil {
		return err
	}
	m.notify()
	return nil
}

func normalizeEnvMap(env map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range env {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		out[k] = v
	}
	return out
}

func (m *Manager) UpsertTask(input Task, isNew bool) (Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	input.Name = strings.TrimSpace(input.Name)
	input.Command = strings.TrimSpace(input.Command)
	input.CronExpr = strings.TrimSpace(input.CronExpr)
	input.Env = normalizeEnvMap(input.Env)
	if input.Name == "" {
		return Task{}, errors.New("name is required")
	}
	if input.Command == "" {
		return Task{}, errors.New("command is required")
	}
	if input.Type != TaskTypeLongRunning && input.Type != TaskTypeOneShot {
		return Task{}, errors.New("invalid task type")
	}
	now := time.Now()
	if isNew {
		input.ID = newID()
		input.CreatedAt = now
		input.UpdatedAt = now
		m.cfg.Tasks = append(m.cfg.Tasks, input)
		m.states[input.ID] = &taskState{TaskRuntimeState: TaskRuntimeState{
			TaskID:  input.ID,
			Enabled: input.Enabled,
			Status:  disabledOrIdle(input.Enabled),
		}}
	} else {
		idx := m.findTaskIndexLocked(input.ID)
		if idx < 0 {
			return Task{}, errors.New("task not found")
		}
		input.CreatedAt = m.cfg.Tasks[idx].CreatedAt
		input.UpdatedAt = now
		m.cfg.Tasks[idx] = input
		st := m.states[input.ID]
		if st == nil {
			st = &taskState{TaskRuntimeState: TaskRuntimeState{TaskID: input.ID}}
			m.states[input.ID] = st
		}
		st.Enabled = input.Enabled
		if !input.Enabled {
			st.Status = "disabled"
		}
	}
	if err := m.persistLocked(); err != nil {
		return Task{}, err
	}
	if err := m.reconcileLocked(); err != nil {
		return Task{}, err
	}
	return input, nil
}

func (m *Manager) DeleteTask(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	idx := m.findTaskIndexLocked(id)
	if idx < 0 {
		return errors.New("task not found")
	}
	if p, ok := m.procs[id]; ok && p.cancel != nil {
		p.cancel()
	}
	if entry, ok := m.entries[id]; ok {
		m.cron.Remove(entry)
		delete(m.entries, id)
	}
	delete(m.procs, id)
	delete(m.states, id)
	m.cfg.Tasks = append(m.cfg.Tasks[:idx], m.cfg.Tasks[idx+1:]...)
	_ = os.Remove(m.store.TaskLogPath(id))
	if err := m.persistLocked(); err != nil {
		return err
	}
	m.notify()
	return nil
}

func (m *Manager) ToggleTask(id string, enabled bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	idx := m.findTaskIndexLocked(id)
	if idx < 0 {
		return errors.New("task not found")
	}
	m.cfg.Tasks[idx].Enabled = enabled
	m.cfg.Tasks[idx].UpdatedAt = time.Now()
	if st, ok := m.states[id]; ok {
		st.Enabled = enabled
		st.Status = disabledOrIdle(enabled)
	}
	if err := m.persistLocked(); err != nil {
		return err
	}
	return m.reconcileLocked()
}

func (m *Manager) StartTask(id string) error {
	go m.startTask(id, "manual")
	return nil
}

func (m *Manager) StopTask(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.procs[id]
	if !ok {
		return errors.New("task is not running")
	}
	if p.cancel != nil {
		p.cancel()
	}
	if st, ok := m.states[id]; ok {
		st.Status = "stopping"
	}
	m.notify()
	return nil
}

func (m *Manager) runScheduled(taskID string) {
	m.mu.RLock()
	idx := m.findTaskIndexLocked(taskID)
	if idx < 0 {
		m.mu.RUnlock()
		return
	}
	t := m.cfg.Tasks[idx]
	m.mu.RUnlock()

	if !t.Enabled {
		return
	}
	if t.Type == TaskTypeLongRunning {
		m.mu.Lock()
		if running, ok := m.procs[taskID]; ok {
			if !t.KillPreviousOnRun {
				m.logSystem(taskID, "[scheduler] skipped trigger because previous run is still active")
				if st, ok := m.states[taskID]; ok {
					st.Status = "skipped"
				}
				m.notify()
				m.mu.Unlock()
				_ = running
				return
			}
			if running.cancel != nil {
				running.cancel()
			}
			runID := running.runID
			m.logSystem(taskID, "[scheduler] old run cancel requested, will start after it exits")
			m.mu.Unlock()
			go m.waitThenStart(taskID, runID)
			return
		}
		m.mu.Unlock()
	}
	go m.startTask(taskID, "cron")
}

func (m *Manager) waitThenStart(taskID, oldRunID string) {
	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.NewTimer(45 * time.Second)
	defer timeout.Stop()

	for {
		select {
		case <-ticker.C:
			m.mu.RLock()
			p, ok := m.procs[taskID]
			m.mu.RUnlock()
			if !ok || p.runID != oldRunID {
				go m.startTask(taskID, "cron-restart")
				return
			}
		case <-timeout.C:
			m.logSystem(taskID, "[scheduler] restart timeout waiting for old run to exit")
			return
		}
	}
}

func (m *Manager) findTaskByID(id string) (Task, bool) {
	for _, t := range m.cfg.Tasks {
		if t.ID == id {
			return t, true
		}
	}
	return Task{}, false
}

func (m *Manager) startTask(id, trigger string) {
	m.mu.Lock()
	task, ok := m.findTaskByID(id)
	if !ok {
		m.mu.Unlock()
		return
	}
	if !task.Enabled && trigger != "manual" {
		m.mu.Unlock()
		return
	}
	if task.Type == TaskTypeLongRunning {
		if _, running := m.procs[id]; running {
			m.mu.Unlock()
			return
		}
	}
	st := m.states[id]
	if st == nil {
		st = &taskState{TaskRuntimeState: TaskRuntimeState{TaskID: id, Enabled: task.Enabled}}
		m.states[id] = st
	}
	st.Running = true
	st.Status = "running"
	now := time.Now()
	st.LastRunStart = &now
	st.LastRunEnd = nil
	st.LastError = ""
	st.LastExitCode = nil
	m.notify()

	ctx, cancel := context.WithCancel(context.Background())
	cmd := shellCommand(ctx, task.Command)
	cmd.Env = mergedEnv(os.Environ(), m.cfg.GlobalEnv, task.Env)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		st.Running = false
		st.Status = "failed"
		st.LastError = err.Error()
		m.mu.Unlock()
		m.logSystem(task.ID, "[error] cannot get stdout: "+err.Error())
		m.notify()
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		st.Running = false
		st.Status = "failed"
		st.LastError = err.Error()
		m.mu.Unlock()
		m.logSystem(task.ID, "[error] cannot get stderr: "+err.Error())
		m.notify()
		return
	}
	runID := newID()
	proc := &activeProc{cmd: cmd, cancel: cancel, runID: runID, taskID: task.ID, isLongRun: task.Type == TaskTypeLongRunning}
	if task.Type == TaskTypeLongRunning {
		m.procs[id] = proc
	}
	m.mu.Unlock()

	m.logSystem(task.ID, fmt.Sprintf("[start] trigger=%s command=%q", trigger, task.Command))
	if err := cmd.Start(); err != nil {
		m.mu.Lock()
		if task.Type == TaskTypeLongRunning {
			delete(m.procs, id)
		}
		st := m.states[id]
		if st != nil {
			st.Running = false
			st.Status = "failed"
			st.LastError = err.Error()
			n := time.Now()
			st.LastRunEnd = &n
		}
		m.mu.Unlock()
		m.logSystem(task.ID, "[error] start failed: "+err.Error())
		m.notify()
		return
	}

	m.mu.Lock()
	if st := m.states[id]; st != nil {
		if cmd.Process != nil {
			st.Pid = cmd.Process.Pid
		}
	}
	m.mu.Unlock()
	m.notify()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		m.streamToLog(task.ID, "stdout", stdout)
	}()
	go func() {
		defer wg.Done()
		m.streamToLog(task.ID, "stderr", stderr)
	}()

	waitErr := cmd.Wait()
	wg.Wait()
	cancel()

	m.mu.Lock()
	if task.Type == TaskTypeLongRunning {
		if p, ok := m.procs[id]; ok && p.runID == runID {
			delete(m.procs, id)
		}
	}
	st = m.states[id]
	if st != nil {
		st.Running = false
		st.Pid = 0
		n := time.Now()
		st.LastRunEnd = &n
		if waitErr == nil {
			code := 0
			st.LastExitCode = &code
			if task.Enabled {
				st.Status = "success"
			} else {
				st.Status = "disabled"
			}
		} else {
			exitCode := -1
			var ee *exec.ExitError
			if errors.As(waitErr, &ee) {
				exitCode = ee.ExitCode()
			}
			st.LastExitCode = &exitCode
			st.LastError = waitErr.Error()
			if ctx.Err() == context.Canceled {
				st.Status = "stopped"
			} else {
				st.Status = "failed"
			}
		}
	}
	m.mu.Unlock()

	if waitErr == nil {
		m.logSystem(task.ID, "[end] exit=0")
	} else {
		m.logSystem(task.ID, "[end] error="+waitErr.Error())
	}
	m.notify()
}

func (m *Manager) streamToLog(taskID, stream string, r io.Reader) {
	scanner := bufio.NewScanner(r)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	for scanner.Scan() {
		m.logLine(taskID, stream, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		m.logSystem(taskID, "[log-error] "+err.Error())
	}
}

func (m *Manager) logSystem(taskID, line string) {
	m.logLine(taskID, "system", line)
}

func (m *Manager) logLine(taskID, stream, line string) {
	m.logMu.Lock()
	defer m.logMu.Unlock()

	path := m.store.TaskLogPath(taskID)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	ts := time.Now().Format(time.RFC3339)
	_, _ = f.WriteString(fmt.Sprintf("%s [%s] %s\n", ts, stream, line))
	_ = f.Close()
	_ = trimLogFileToLastNLines(path, maxLogLinesPerTask)
}

func trimLogFileToLastNLines(path string, keep int) error {
	if keep <= 0 {
		return os.WriteFile(path, []byte{}, 0o644)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if len(b) == 0 {
		return nil
	}
	trimmed := bytes.TrimRight(b, "\n")
	if len(trimmed) == 0 {
		return nil
	}
	lines := bytes.Split(trimmed, []byte("\n"))
	if len(lines) <= keep {
		return nil
	}
	lines = lines[len(lines)-keep:]
	out := bytes.Join(lines, []byte("\n"))
	out = append(out, '\n')
	return os.WriteFile(path, out, 0o644)
}

func (m *Manager) mergedConfigLocked() AppConfig {
	out := AppConfig{
		GlobalEnv: map[string]string{},
		Tasks:     make([]Task, 0, len(m.cfg.Tasks)),
	}
	for k, v := range m.cfg.GlobalEnv {
		out.GlobalEnv[k] = v
	}
	out.Tasks = append(out.Tasks, m.cfg.Tasks...)
	return out
}

func mergedEnv(base []string, global, local map[string]string) []string {
	m := map[string]string{}
	for _, kv := range base {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) == 2 {
			m[parts[0]] = parts[1]
		}
	}
	for k, v := range global {
		m[k] = v
	}
	for k, v := range local {
		m[k] = v
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k+"="+m[k])
	}
	return out
}

func shellCommand(ctx context.Context, command string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.CommandContext(ctx, "cmd", "/C", command)
	}
	return exec.CommandContext(ctx, "sh", "-c", command)
}

func (m *Manager) ExportJSON() ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	payload := struct {
		Config AppConfig       `json:"config"`
		Tasks  []TaskWithState `json:"tasks"`
	}{
		Config: m.mergedConfigLocked(),
		Tasks:  m.ListTasks(),
	}
	return json.Marshal(payload)
}

func (m *Manager) ReadTaskLogs(taskID string, offset int64, limit int64) (string, int64, bool, error) {
	m.logMu.Lock()
	defer m.logMu.Unlock()

	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = 256 * 1024
	}
	path := m.store.TaskLogPath(taskID)
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", 0, true, nil
		}
		return "", 0, false, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return "", 0, false, err
	}
	size := fi.Size()
	if offset > size {
		offset = 0
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return "", size, false, err
	}
	buf := bytes.NewBuffer(nil)
	_, err = io.CopyN(buf, f, limit)
	if err != nil && !errors.Is(err, io.EOF) {
		return "", size, false, err
	}
	newOffset := offset + int64(buf.Len())
	eof := newOffset >= size
	return buf.String(), newOffset, eof, nil
}
