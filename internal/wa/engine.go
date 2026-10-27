package wa

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// 超过这个宽限期就不再补发，避免 CPA 重启后打出一堆过期请求。
const scheduleGrace = 2 * time.Minute

type Engine struct {
	mu        sync.Mutex
	host      Host
	now       func() time.Time
	cfg       Config
	state     diskState
	loadedAt  string
	accounts  map[string]Account
	busy      map[string]bool
	lastError string
	running   bool
	closed    bool
	started   bool
	changed   chan struct{}
	stop      chan struct{}
	workers   sync.WaitGroup
}

func NewEngine(host Host, now func() time.Time) *Engine {
	if now == nil {
		now = time.Now
	}
	return &Engine{
		host:     host,
		now:      now,
		cfg:      DefaultConfig(),
		accounts: map[string]Account{},
		busy:     map[string]bool{},
		changed:  make(chan struct{}, 1),
		stop:     make(chan struct{}),
	}
}

func (e *Engine) Apply(cfg Config) error {
	cfg, err := ValidateConfig(cfg)
	if err != nil {
		return err
	}
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return errors.New("插件正在关闭")
	}
	if e.loadedAt != cfg.StateFile {
		if e.running {
			e.mu.Unlock()
			return errors.New("运行期间不能修改 state_file")
		}
		st, errRead := readState(cfg.StateFile)
		if errRead != nil {
			e.lastError = errRead.Error()
			e.mu.Unlock()
			return errRead
		}
		e.state = st
		e.loadedAt = cfg.StateFile
	}
	e.cfg = cfg
	e.lastError = ""
	if !e.started {
		e.started = true
		e.workers.Add(1)
		go e.loop()
	}
	e.mu.Unlock()
	e.wake()
	// 同步拉一次账号列表：注册完成时第一次 Status 就能显示账号名，而不是 auth_id
	e.RefreshAccounts()
	return nil
}

func (e *Engine) Invalidate(message string) {
	e.mu.Lock()
	e.lastError = message
	e.mu.Unlock()
	e.wake()
}

func (e *Engine) Close() {
	e.mu.Lock()
	if !e.closed {
		e.closed = true
		close(e.stop)
	}
	e.mu.Unlock()
	e.workers.Wait()
}

func (e *Engine) wake() {
	select {
	case e.changed <- struct{}{}:
	default:
	}
}

// RefreshAccounts 缓存 CPA 的账号列表，只用于显示名字和状态。
func (e *Engine) RefreshAccounts() {
	list, err := e.Accounts()
	if err != nil {
		return
	}
	index := make(map[string]Account, len(list))
	for _, a := range list {
		index[a.ID] = a
	}
	e.mu.Lock()
	e.accounts = index
	e.mu.Unlock()
}

func (e *Engine) Accounts() ([]Account, error) {
	var response struct {
		Files []Account `json:"files"`
	}
	if err := e.host.Call("host.auth.list", struct{}{}, &response); err != nil {
		return nil, errors.New("无法从 CPA 获取账号列表")
	}
	result := []Account{}
	for _, account := range response.Files {
		if account.ID == "" {
			continue
		}
		provider := account.Provider
		if provider == "" {
			provider = account.Type
		}
		account.Provider = strings.ToLower(provider)
		result = append(result, account)
	}
	return result, nil
}

// loop 是所有账号共用的一条时间线：算出最近的一次触发，等到点，只打那一个账号。
func (e *Engine) loop() {
	defer e.workers.Done()
	for {
		e.mu.Lock()
		active := !e.closed && e.cfg.Enabled && e.lastError == ""
		cfg := e.cfg
		e.mu.Unlock()

		index, at, ok := -1, time.Time{}, false
		if active {
			now := e.now()
			for i, plan := range cfg.Plans {
				if !plan.On() || len(plan.Times) == 0 {
					continue
				}
				candidate, has := NextAt(now, cfg.Timezone, plan.Times)
				if !has {
					continue
				}
				if !ok || candidate.Before(at) {
					index, at, ok = i, candidate, true
				}
			}
		}

		if !ok {
			select {
			case <-e.stop:
				return
			case <-e.changed:
				continue
			}
		}

		timer := time.NewTimer(at.Sub(e.now()))
		select {
		case <-e.stop:
			timer.Stop()
			return
		case <-e.changed:
			timer.Stop()
			continue
		case <-timer.C:
			e.fire(index, at)
		}
	}
}

func (e *Engine) fire(index int, at time.Time) {
	e.mu.Lock()
	if e.closed || index < 0 || index >= len(e.cfg.Plans) {
		e.mu.Unlock()
		return
	}
	plan := e.cfg.Plans[index]
	e.mu.Unlock()

	if !plan.On() {
		return
	}
	if e.now().Sub(at) > scheduleGrace {
		e.record(plan, at, "missed", "已错过触发时间，本次不补发", 0)
		return
	}
	e.runPlan(plan, at, "scheduled", false)
}

func (e *Engine) runPlan(plan Plan, at time.Time, mode string, force bool) {
	e.mu.Lock()
	if e.closed || e.busy[plan.AuthID] {
		e.mu.Unlock()
		return
	}
	if !force && (!e.cfg.Enabled || (mode == "scheduled" && !plan.On())) {
		e.mu.Unlock()
		return
	}
	e.busy[plan.AuthID] = true
	e.running = mode == "manual"
	e.mu.Unlock()

	defer func() {
		e.mu.Lock()
		e.busy[plan.AuthID] = false
		e.running = false
		e.mu.Unlock()
	}()

	key := fmt.Sprintf("%s:%d:%s", mode, at.Unix(), plan.AuthID)
	if mode == "manual" {
		key = fmt.Sprintf("manual:%d:%s", e.now().UnixNano(), plan.AuthID)
	}
	result, ok := e.claim(key, plan, mode, at)
	if !ok {
		return
	}
	if mode == "scheduled" && e.now().Sub(at) > scheduleGrace {
		result.Status = "missed"
		result.Message = "已错过触发时间，本次不补发"
		result.CompletedAt = e.now()
		e.finish(result)
		return
	}
	result = e.execute(result)
	result.CompletedAt = e.now()
	e.finish(result)
}

func (e *Engine) record(plan Plan, at time.Time, status, message string, httpStatus int) {
	result, ok := e.claim(fmt.Sprintf("scheduled:%d:%s", at.Unix(), plan.AuthID), plan, "scheduled", at)
	if !ok {
		return
	}
	result.Status = status
	result.Message = message
	result.HTTPStatus = httpStatus
	result.CompletedAt = e.now()
	e.finish(result)
}

func (e *Engine) claim(key string, plan Plan, mode string, at time.Time) (Result, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, exists := e.state.Claims[key]; exists {
		return Result{}, false
	}
	now := e.now()
	label := plan.AuthID
	if acc, ok := e.accounts[plan.AuthID]; ok {
		label = acc.DisplayName()
	}
	result := Result{
		ID: key, AuthID: plan.AuthID, Provider: plan.Provider, Label: label, Model: plan.Model,
		Mode: mode, ScheduledFor: at, StartedAt: now, Status: "running", Message: "正在触发",
	}
	for claim, timestamp := range e.state.Claims {
		if now.Sub(timestamp) > 14*24*time.Hour {
			delete(e.state.Claims, claim)
		}
	}
	e.state.Claims[key] = now
	e.state.Results = append(e.state.Results, result)
	if len(e.state.Results) > 500 {
		e.state.Results = append([]Result{}, e.state.Results[len(e.state.Results)-500:]...)
	}
	if err := writeState(e.loadedAt, e.state); err != nil {
		e.lastError = err.Error()
		return Result{}, false
	}
	return result, true
}

func (e *Engine) finish(result Result) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i := len(e.state.Results) - 1; i >= 0; i-- {
		if e.state.Results[i].ID == result.ID {
			e.state.Results[i] = result
			break
		}
	}
	if err := writeState(e.loadedAt, e.state); err != nil {
		e.lastError = err.Error()
		return false
	}
	return true
}

func (e *Engine) execute(result Result) Result {
	payload := map[string]any{
		"model":            result.Model,
		"stream":           false,
		"reasoning_effort": "low",
		"messages":         []map[string]string{{"role": "user", "content": "Reply with exactly: OK"}},
	}
	body, _ := json.Marshal(payload)
	request := ModelRequest{
		EntryProtocol:  "openai",
		ExitProtocol:   "openai",
		Model:          result.Model,
		Stream:         false,
		Body:           body,
		ForcedProvider: result.Provider,
		AuthID:         result.AuthID,
	}
	var response ModelResponse
	if err := e.host.Call("host.model.execute", request, &response); err != nil {
		var hostErr *HostError
		if errors.As(err, &hostErr) {
			result.HTTPStatus = hostErr.StatusCode
		}
		result.Status = "failed"
		result.Message = failureMessage(result.HTTPStatus)
		return result
	}
	result.HTTPStatus = response.StatusCode
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		result.Status = "failed"
		result.Message = failureMessage(response.StatusCode)
		return result
	}
	var completion struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error json.RawMessage `json:"error"`
	}
	if json.Unmarshal(response.Body, &completion) != nil ||
		(len(completion.Error) > 0 && string(completion.Error) != "null") ||
		len(completion.Choices) == 0 ||
		strings.TrimSpace(completion.Choices[0].Message.Content) == "" {
		result.Status = "uncertain"
		result.Message = "上游返回成功但没有有效回复，本次不重复发送"
		return result
	}
	result.ResetAt, result.UsedPercent = FiveHourQuota(response.Headers, e.now())
	result.Status = "success"
	if result.ResetAt != nil {
		result.Message = "请求成功，已读到新的 5h 窗口重置时间"
	} else {
		result.Message = "请求成功，但上游没有返回 5h 重置时间"
	}
	return result
}

func failureMessage(status int) string {
	switch status {
	case 401, 403:
		return "账号认证失败或没有该模型权限"
	case 429:
		return "额度不足或被上游限流"
	case 400, 404:
		return "模型或请求不被上游支持，检查 model 填得对不对"
	default:
		return "CPA 或上游请求失败"
	}
}

func (e *Engine) Status() Status {
	e.mu.Lock()
	defer e.mu.Unlock()
	now := e.now()
	status := Status{
		Version:   Version,
		Enabled:   e.cfg.Enabled,
		Timezone:  e.cfg.Timezone,
		Running:   e.running,
		LastError: e.lastError,
		Results:   append([]Result{}, e.state.Results...),
		Now:       now,
	}
	for _, plan := range e.cfg.Plans {
		item := PlanStatus{
			Provider: plan.Provider,
			AuthID:  plan.AuthID,
			Label:   plan.AuthID,
			Model:   plan.Model,
			Times:   append([]string{}, plan.Times...),
			Enabled: plan.On(),
		}
		if acc, ok := e.accounts[plan.AuthID]; ok {
			item.Label = acc.DisplayName()
			item.Disabled = acc.Disabled || acc.Status == "disabled"
		} else if len(e.accounts) > 0 {
			// 账号列表尚未加载成功时不要误报"账号不存在"
			item.Missing = true
		}
		if plan.On() && len(plan.Times) > 0 && !e.closed && e.cfg.Enabled && e.lastError == "" {
			if at, ok := NextAt(now, e.cfg.Timezone, plan.Times); ok {
				item.NextTrigger = &at
			}
		}
		status.Plans = append(status.Plans, item)
	}
	return status
}

// DeleteResults 按 id 删除执行记录；运行中的记录不删，避免状态错乱。
func (e *Engine) DeleteResults(ids []string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(ids) == 0 {
		return 0
	}
	remove := make(map[string]bool, len(ids))
	for _, id := range ids {
		remove[id] = true
	}
	kept := make([]Result, 0, len(e.state.Results))
	deleted := 0
	for _, row := range e.state.Results {
		if remove[row.ID] && row.Status != "running" {
			deleted++
			continue
		}
		kept = append(kept, row)
	}
	if deleted == 0 {
		return 0
	}
	e.state.Results = kept
	if err := writeState(e.loadedAt, e.state); err != nil {
		e.lastError = err.Error()
	}
	return deleted
}

// Manual 立即触发：authID 为空时触发所有启用的账号。
func (e *Engine) Manual(authID string) (int, error) {
	e.mu.Lock()
	if e.closed || !e.cfg.Enabled {
		e.mu.Unlock()
		return 0, errors.New("插件未启用")
	}
	if e.lastError != "" {
		message := e.lastError
		e.mu.Unlock()
		return 0, errors.New(message)
	}
	plans := append([]Plan{}, e.cfg.Plans...)
	e.mu.Unlock()

	now := e.now()
	count := 0
	for _, plan := range plans {
		if authID != "" && plan.AuthID != authID {
			continue
		}
		// 没有配置时间的计划不参与触发，否则会打出一个无意义的请求
		if len(plan.Times) == 0 {
			continue
		}
		e.workers.Add(1)
		count++
		go func(p Plan) {
			defer e.workers.Done()
			e.runPlan(p, now, "manual", true)
		}(plan)
	}
	if count == 0 {
		return 0, errors.New("没有匹配的计划")
	}
	return count, nil
}
