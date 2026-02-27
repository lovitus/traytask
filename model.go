package main

import "time"

type TaskType string

const (
	TaskTypeLongRunning TaskType = "long_running"
	TaskTypeOneShot     TaskType = "one_shot"
)

type Task struct {
	ID                string            `json:"id"`
	Name              string            `json:"name"`
	Command           string            `json:"command"`
	Type              TaskType          `json:"type"`
	Enabled           bool              `json:"enabled"`
	CronExpr          string            `json:"cronExpr,omitempty"`
	KillPreviousOnRun bool              `json:"killPreviousOnRun"`
	Env               map[string]string `json:"env,omitempty"`
	CreatedAt         time.Time         `json:"createdAt"`
	UpdatedAt         time.Time         `json:"updatedAt"`
}

type AppConfig struct {
	GlobalEnv map[string]string `json:"globalEnv"`
	Tasks     []Task            `json:"tasks"`
}

type TaskRuntimeState struct {
	TaskID       string     `json:"taskId"`
	Running      bool       `json:"running"`
	Enabled      bool       `json:"enabled"`
	Status       string     `json:"status"`
	LastRunStart *time.Time `json:"lastRunStart,omitempty"`
	LastRunEnd   *time.Time `json:"lastRunEnd,omitempty"`
	LastExitCode *int       `json:"lastExitCode,omitempty"`
	LastError    string     `json:"lastError,omitempty"`
	Pid          int        `json:"pid,omitempty"`
	NextRun      *time.Time `json:"nextRun,omitempty"`
}

type TaskWithState struct {
	Task  Task             `json:"task"`
	State TaskRuntimeState `json:"state"`
}
