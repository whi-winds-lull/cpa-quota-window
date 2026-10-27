package wa

import (
	"net/http"
	"net/url"
	"time"
)

// Host 是插件调用 CPA 宿主的唯一通道。
type Host interface{ Call(method string, request any, out any) error }

type HostError struct {
	StatusCode int
	Code       string
}

func (e *HostError) Error() string { return "CPA 宿主调用失败" }

type Account struct {
	ID          string `json:"id"`
	AuthIndex   string `json:"auth_index,omitempty"`
	Name        string `json:"name"`
	Provider    string `json:"provider"`
	Type        string `json:"type,omitempty"`
	Label       string `json:"label,omitempty"`
	Email       string `json:"email,omitempty"`
	PlanType    string `json:"plan_type,omitempty"`
	Status      string `json:"status,omitempty"`
	Disabled    bool   `json:"disabled"`
	Unavailable bool   `json:"unavailable"`
}

func (a Account) DisplayName() string {
	if a.Label != "" {
		return a.Label
	}
	if a.Email != "" {
		return a.Email
	}
	if a.Name != "" {
		return a.Name
	}
	return a.ID
}

type ModelRequest struct {
	EntryProtocol  string      `json:"entry_protocol"`
	ExitProtocol   string      `json:"exit_protocol"`
	Model          string      `json:"model"`
	Stream         bool        `json:"stream"`
	Body           []byte      `json:"body"`
	Headers        http.Header `json:"headers"`
	Query          url.Values  `json:"query"`
	Alt            string      `json:"alt"`
	ForcedProvider string      `json:"forced_provider"`
	AuthID         string      `json:"auth_id"`
}

type ModelResponse struct {
	StatusCode int         `json:"status_code"`
	Headers    http.Header `json:"headers"`
	Body       []byte      `json:"body"`
}

type Result struct {
	ID           string     `json:"id"`
	AuthID       string     `json:"auth_id"`
	Provider     string     `json:"provider,omitempty"`
	Label        string     `json:"label"`
	Model        string     `json:"model"`
	Mode         string     `json:"mode"`
	ScheduledFor time.Time  `json:"scheduled_for"`
	StartedAt    time.Time  `json:"started_at"`
	CompletedAt  time.Time  `json:"completed_at"`
	Status       string     `json:"status"`
	HTTPStatus   int        `json:"http_status,omitempty"`
	Message      string     `json:"message"`
	ResetAt      *time.Time `json:"reset_at,omitempty"`
	UsedPercent  *float64   `json:"used_percent,omitempty"`
}

// PlanStatus 是给前端看的单账号计划视图：一个账号一行，各行时间互不影响。
type PlanStatus struct {
	Provider    string     `json:"provider"`
	AuthID      string     `json:"auth_id"`
	Label       string     `json:"label"`
	Model       string     `json:"model"`
	Times       []string   `json:"times"`
	Enabled     bool       `json:"enabled"`
	Missing     bool       `json:"missing"`
	Disabled    bool       `json:"disabled"`
	NextTrigger *time.Time `json:"next_trigger,omitempty"`
}

type Status struct {
	Version   string       `json:"version"`
	Enabled   bool         `json:"enabled"`
	Timezone  string       `json:"timezone"`
	Plans     []PlanStatus `json:"plans"`
	Running   bool         `json:"running"`
	LastError string       `json:"last_error,omitempty"`
	Results   []Result     `json:"results"`
	Now       time.Time    `json:"now"`
}
