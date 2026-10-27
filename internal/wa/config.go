package wa

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	_ "time/tzdata"

	"gopkg.in/yaml.v3"
)

const PluginID = "window-anchor"
const Version = "1.0.0"

// Plan 是一个账号的独立计划：它有自己的时间表和模型，与其他账号互不影响。
// Provider 区分 Codex 与 Antigravity，两者共用同一套锚定逻辑。
type Plan struct {
	Provider string   `json:"provider" yaml:"provider"`
	AuthID   string   `json:"auth_id" yaml:"auth_id"`
	Model    string   `json:"model" yaml:"model"`
	Times    []string `json:"times" yaml:"times"`
	Enabled  *bool    `json:"enabled,omitempty" yaml:"enabled,omitempty"`
}

const (
	ProviderCodex       = "codex"
	ProviderAntigravity = "antigravity"
)

func (p Plan) On() bool { return p.Enabled == nil || *p.Enabled }

type Config struct {
	Enabled   bool   `json:"enabled" yaml:"enabled"`
	Timezone  string `json:"timezone" yaml:"timezone"`
	Plans     []Plan `json:"plans" yaml:"plans"`
	StateFile string `json:"-" yaml:"state_file"`
}

func DefaultConfig() Config {
	return Config{
		Enabled:   true,
		Timezone:  "Asia/Shanghai",
		Plans:     []Plan{},
		StateFile: "plugins/state/window-anchor.json",
	}
}

func ParseConfig(raw []byte) (Config, error) {
	cfg := DefaultConfig()
	if len(raw) > 256*1024 {
		return cfg, errors.New("插件配置过大")
	}
	if len(raw) > 0 {
		if err := yaml.Unmarshal(raw, &cfg); err != nil {
			return cfg, errors.New("插件配置不是有效 YAML")
		}
	}
	return ValidateConfig(cfg)
}

func ValidateConfig(cfg Config) (Config, error) {
	cfg.Timezone = strings.TrimSpace(cfg.Timezone)
	if cfg.Timezone == "" {
		return cfg, errors.New("timezone 不能为空，请填写 IANA 时区，例如 Asia/Shanghai")
	}
	if _, err := time.LoadLocation(cfg.Timezone); err != nil {
		return cfg, fmt.Errorf("无效的 IANA 时区：%s", cfg.Timezone)
	}
	if strings.TrimSpace(cfg.StateFile) == "" {
		cfg.StateFile = DefaultConfig().StateFile
	}
	seen := map[string]bool{}
	out := make([]Plan, 0, len(cfg.Plans))
	for i, plan := range cfg.Plans {
		plan.Provider = strings.ToLower(strings.TrimSpace(plan.Provider))
		if plan.Provider == "" {
			plan.Provider = ProviderCodex // 兼容旧配置：没写 provider 就当 codex
		}
		if plan.Provider != ProviderCodex && plan.Provider != ProviderAntigravity {
			return cfg, fmt.Errorf("第 %d 条计划的 provider 只支持 codex 或 antigravity", i+1)
		}
		plan.AuthID = strings.TrimSpace(plan.AuthID)
		plan.Model = strings.TrimSpace(plan.Model)
		if plan.AuthID == "" {
			return cfg, fmt.Errorf("第 %d 条计划缺少 auth_id", i+1)
		}
		key := plan.Provider + "|" + plan.AuthID
		if seen[key] {
			return cfg, fmt.Errorf("账号 %s 在 %s 下重复配置", plan.AuthID, plan.Provider)
		}
		seen[key] = true
		if len(plan.Times) > 24 {
			return cfg, fmt.Errorf("账号 %s 每天最多 24 个时间点", plan.AuthID)
		}
		if plan.Model == "" {
			return cfg, fmt.Errorf("账号 %s 缺少 model", plan.AuthID)
		}
		if strings.ContainsAny(plan.Model, " \t\r\n") || len(plan.Model) > 160 {
			return cfg, fmt.Errorf("账号 %s 的 model 含非法字符", plan.AuthID)
		}
		clean := make([]string, 0, len(plan.Times))
		dedup := map[string]bool{}
		for _, value := range plan.Times {
			value = strings.TrimSpace(value)
			clock, err := time.Parse("15:04", value)
			if err != nil || clock.Format("15:04") != value {
				return cfg, fmt.Errorf("账号 %s 的时间 %q 必须使用 HH:MM 格式", plan.AuthID, value)
			}
			if !dedup[value] {
				clean = append(clean, value)
				dedup[value] = true
			}
		}
		sort.Strings(clean)
		plan.Times = clean
		out = append(out, plan)
	}
	cfg.Plans = out
	return cfg, nil
}

// NextAt 计算一组时间点在 after 之后最近的一次触发（按 timezone 解释）。
func NextAt(after time.Time, timezone string, times []string) (time.Time, bool) {
	loc, err := time.LoadLocation(timezone)
	if err != nil || len(times) == 0 {
		return time.Time{}, false
	}
	local := after.In(loc)
	for day := 0; day < 3; day++ {
		date := time.Date(local.Year(), local.Month(), local.Day()+day, 12, 0, 0, 0, loc)
		var best time.Time
		for _, value := range times {
			clock, err := time.Parse("15:04", value)
			if err != nil {
				continue
			}
			at := time.Date(date.Year(), date.Month(), date.Day(), clock.Hour(), clock.Minute(), 0, 0, loc)
			if at.Hour() != clock.Hour() || at.Minute() != clock.Minute() || at.Day() != date.Day() {
				continue
			}
			if at.After(after) && (best.IsZero() || at.Before(best)) {
				best = at
			}
		}
		if !best.IsZero() {
			return best, true
		}
	}
	return time.Time{}, false
}
