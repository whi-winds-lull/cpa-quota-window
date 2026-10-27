package wa

import (
	"encoding/json"
	"errors"
	"io/fs"
	"net/url"
	"strings"
)

type Application struct {
	Engine *Engine
	Assets fs.FS
}

type envelope struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type managementRequest struct {
	Method string     `json:"method"`
	Path   string     `json:"path"`
	Query  url.Values `json:"query"`
	Body   []byte     `json:"body"`
}

type managementResponse struct {
	StatusCode int         `json:"status_code"`
	Headers    map[string][]string `json:"headers,omitempty"`
	Body       []byte      `json:"body"`
}

func (a *Application) Call(method string, raw []byte) []byte {
	result, err := a.dispatch(method, raw)
	if err != nil {
		out, _ := json.Marshal(envelope{OK: false, Error: &rpcError{Code: "plugin_error", Message: err.Error()}})
		return out
	}
	payload, _ := json.Marshal(result)
	out, _ := json.Marshal(envelope{OK: true, Result: payload})
	return out
}

func (a *Application) dispatch(method string, raw []byte) (any, error) {
	switch method {
	case "plugin.register", "plugin.reconfigure":
		var req struct {
			ConfigYAML []byte `json:"config_yaml"`
			Schema     uint32 `json:"schema_version"`
		}
		if err := json.Unmarshal(raw, &req); err != nil {
			return nil, errors.New("注册请求无效")
		}
		cfg, err := ParseConfig(req.ConfigYAML)
		if err != nil {
			a.Engine.Invalidate(err.Error())
			return nil, err
		}
		if err := a.Engine.Apply(cfg); err != nil {
			a.Engine.Invalidate(err.Error())
			return nil, err
		}
		return a.registration(req.Schema), nil
	case "plugin.quiesce":
		// quiesce 是临时静默，不能 Close，否则后续 reconfigure 会被"插件正在关闭"拒绝
		return struct{}{}, nil
	case "plugin.shutdown":
		a.Engine.Close()
		return struct{}{}, nil
	case "management.register":
		return map[string]any{
			"resources": []map[string]string{
				// Path 用 /status 而不是 /，和菜单渲染逻辑保持一致
				{"Path": "/status", "Menu": "账号窗口锚定", "Description": "按账号设置每日 5h 窗口锚定时间"},
				{"Path": "/status.html"},
				{"Path": "/app.js"},
				{"Path": "/ui.js"},
				{"Path": "/i18n.js"},
				{"Path": "/history.js"},
				{"Path": "/cpa-session.js"},
				{"Path": "/timezone-picker.js"},
				{"Path": "/timezone-picker.css"},
				{"Path": "/style.css"},
				{"Path": "/openai.png"},
				{"Path": "/gemini.svg"},
				{"Path": "/status.json"},
				// 账号列表也走免鉴权资源：只读、无敏感信息，
				// 这样没有管理密钥时页面也能把账号列出来供参考
				{"Path": "/accounts.json"},
			},
			"routes": []map[string]string{
				{"Method": "GET", "Path": "/plugins/" + PluginID + "/status"},
				{"Method": "GET", "Path": "/plugins/" + PluginID + "/accounts"},
				{"Method": "POST", "Path": "/plugins/" + PluginID + "/run"},
				{"Method": "POST", "Path": "/plugins/" + PluginID + "/history/delete"},
			},
		}, nil
	case "management.handle":
		return a.handle(raw)
	}
	return nil, errors.New("插件不支持这个方法")
}

func (a *Application) registration(schema uint32) any {
	if schema == 0 {
		schema = 1
	}
	return map[string]any{
		"schema_version": schema,
		"metadata": map[string]any{
			"Name":             PluginID,
			"Version":          Version,
			"Author":           "whi-winds-lull",
			// CPA 的 validPlugin 会校验这四个字段全部非空，
			// GitHubRepository 留空会导致插件"加载成功但注册失败"。
			"GitHubRepository": "https://github.com/whi-winds-lull/cpa-window-starter",
			"Logo":             "",
			"ConfigFields": []map[string]string{
				{"Name": "enabled", "Type": "boolean", "Description": "插件总开关"},
				{"Name": "timezone", "Type": "string", "Description": "IANA 时区，例如 Asia/Shanghai（不能留空）"},
				{"Name": "plans", "Type": "array", "Description": "每个账号一条计划：auth_id / times / model"},
			},
		},
		"capabilities": map[string]bool{"management_api": true},
	}
}

func (a *Application) handle(raw []byte) (any, error) {
	var req managementRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, errors.New("管理请求无效")
	}
	path := req.Path
	// 资源页和 Management API 的路径后缀会重名（都以 /plugins/<id>/status 结尾），
	// 只有带 /v0/management/ 前缀或以 /plugins/ 开头的才算管理接口。
	isAPI := strings.Contains(path, "/v0/management/") || strings.HasPrefix(path, "/plugins/")
	if isAPI {
		switch {
		case strings.HasSuffix(path, "/plugins/"+PluginID+"/status"):
			return a.jsonResponse(a.Engine.Status())
		case strings.HasSuffix(path, "/plugins/"+PluginID+"/accounts"):
			list, err := a.Engine.Accounts()
			if err != nil {
				return nil, err
			}
			return a.jsonResponse(map[string]any{"accounts": list})
		case strings.HasSuffix(path, "/plugins/"+PluginID+"/run"):
			var body struct {
				AuthID string `json:"auth_id"`
			}
			if len(req.Body) > 0 {
				_ = json.Unmarshal(req.Body, &body)
			}
			count, err := a.Engine.Manual(strings.TrimSpace(body.AuthID))
			if err != nil {
				return nil, err
			}
			return a.jsonResponse(map[string]any{"triggered": count})
		case strings.HasSuffix(path, "/plugins/"+PluginID+"/history/delete"):
			var body struct {
				IDs []string `json:"ids"`
			}
			if len(req.Body) > 0 {
				_ = json.Unmarshal(req.Body, &body)
			}
			removed := a.Engine.DeleteResults(body.IDs)
			return a.jsonResponse(map[string]any{"deleted": removed})
		}
	}
	return a.asset(path)
}

// asset 提供资源页与一份免鉴权的只读状态，登录态缺失时也能看到计划。
func (a *Application) asset(path string) (any, error) {
	clean := path
	// 宿主可能传完整路径，也可能只传相对路径，统一剥掉资源前缀
	if i := strings.Index(clean, "/v0/resource/plugins/"+PluginID); i >= 0 {
		clean = clean[i+len("/v0/resource/plugins/"+PluginID):]
	}
	clean = strings.Trim(clean, "/")
	// 资源引用会带 ?v=xxx 的版本参数，取文件名部分即可
	if i := strings.IndexByte(clean, '?'); i >= 0 {
		clean = clean[:i]
	}
	// 入口页：菜单指向 /status，根路径也映射到同一个页面
	if clean == "" || clean == "status" {
		clean = "status.html"
	}
	if strings.Contains(clean, "..") {
		return nil, errors.New("非法路径")
	}
	if clean == "accounts.json" {
		list, err := a.Engine.Accounts()
		if err != nil {
			list = []Account{}
		}
		payload, errMarshal := json.MarshalIndent(map[string]any{"accounts": list}, "", "  ")
		if errMarshal != nil {
			return nil, errMarshal
		}
		return managementResponse{
			StatusCode: 200,
			Headers: map[string][]string{
				"Content-Type":  {"application/json; charset=utf-8"},
				"Cache-Control": {"no-store, must-revalidate"},
			},
			Body: payload,
		}, nil
	}
	if clean == "status.json" {
		payload, err := json.MarshalIndent(a.Engine.Status(), "", "  ")
		if err != nil {
			return nil, err
		}
		return managementResponse{
			StatusCode: 200,
			Headers: map[string][]string{
				"Content-Type":  {"application/json; charset=utf-8"},
				"Cache-Control": {"no-store, must-revalidate"},
			},
			Body: payload,
		}, nil
	}
	if a.Assets == nil {
		return managementResponse{StatusCode: 404, Body: []byte("not found")}, nil
	}
	data, err := fs.ReadFile(a.Assets, clean)
	if err != nil {
		return managementResponse{StatusCode: 404, Body: []byte("not found")}, nil
	}
	return managementResponse{
		StatusCode: 200,
		Headers: map[string][]string{
			"Content-Type":  {contentType(clean)},
			// 插件升级后浏览器不能拿旧的 JS/CSS，否则看到的还是上一版行为
			"Cache-Control": {"no-store, must-revalidate"},
		},
		Body: data,
	}, nil
}

// jsonResponse 把任意结构包成宿主 management.handle 要求的响应格式。
// 直接返回裸 JSON 会被宿主解析失败，表现为 HTTP 200 但 body 为空。
func (a *Application) jsonResponse(value any) (any, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return managementResponse{
		StatusCode: 200,
		Headers: map[string][]string{
			"Content-Type":  {"application/json; charset=utf-8"},
			"Cache-Control": {"no-store, must-revalidate"},
		},
		Body: payload,
	}, nil
}

func contentType(name string) string {	switch {
	case strings.HasSuffix(name, ".html"):
		return "text/html; charset=utf-8"
	case strings.HasSuffix(name, ".js"):
		return "application/javascript; charset=utf-8"
	case strings.HasSuffix(name, ".css"):
		return "text/css; charset=utf-8"
	case strings.HasSuffix(name, ".json"):
		return "application/json; charset=utf-8"
	case strings.HasSuffix(name, ".svg"):
		return "image/svg+xml"
	case strings.HasSuffix(name, ".png"):
		return "image/png"
	case strings.HasSuffix(name, ".ico"):
		return "image/x-icon"
	default:
		return "application/octet-stream"
	}
}
