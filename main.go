package main

/*
#include <stdint.h>
#include <stdlib.h>

typedef struct { void* ptr; size_t len; } cliproxy_buffer;
typedef int (*cliproxy_host_call_fn)(void*, const char*, const uint8_t*, size_t, cliproxy_buffer*);
typedef void (*cliproxy_host_free_fn)(void*, size_t);
typedef struct { uint32_t abi_version; void* host_ctx; cliproxy_host_call_fn call; cliproxy_host_free_fn free_buffer; } cliproxy_host_api;
typedef int (*cliproxy_plugin_call_fn)(char*, uint8_t*, size_t, cliproxy_buffer*);
typedef void (*cliproxy_plugin_free_fn)(void*, size_t);
typedef void (*cliproxy_plugin_shutdown_fn)(void);
typedef struct { uint32_t abi_version; cliproxy_plugin_call_fn call; cliproxy_plugin_free_fn free_buffer; cliproxy_plugin_shutdown_fn shutdown; } cliproxy_plugin_api;
extern int cliproxyPluginCall(char*, uint8_t*, size_t, cliproxy_buffer*);
extern void cliproxyPluginFree(void*, size_t);
extern void cliproxyPluginShutdown(void);
static cliproxy_host_api stored_host;
static int host_ready;
static void store_host_api(const cliproxy_host_api* host) { stored_host = *host; host_ready = 1; }
static int call_host_api(const char* method, const uint8_t* request, size_t len, cliproxy_buffer* response) {
 if (!host_ready || stored_host.call == NULL) return 1;
 return stored_host.call(stored_host.host_ctx, method, request, len, response);
}
static void free_host_buffer(void* ptr, size_t len) {
 if (host_ready && stored_host.free_buffer != NULL && ptr != NULL) stored_host.free_buffer(ptr, len);
}
*/
import "C"

import (
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
	"sync"
	"time"
	"unsafe"

	"cpa-quota-window/internal/wa"
)

//go:embed web/*
var assets embed.FS

var (
	appMu sync.RWMutex
	app   *wa.Application
)

func main() {}

//export cliproxy_plugin_init
func cliproxy_plugin_init(host *C.cliproxy_host_api, plugin *C.cliproxy_plugin_api) C.int {
	if host == nil || plugin == nil || host.abi_version != 1 || host.call == nil || host.free_buffer == nil {
		return 1
	}
	C.store_host_api(host)
	web, _ := fs.Sub(assets, "web")
	appMu.Lock()
	app = &wa.Application{Engine: wa.NewEngine(callbackHost{}, time.Now), Assets: web}
	appMu.Unlock()
	plugin.abi_version = 1
	plugin.call = C.cliproxy_plugin_call_fn(C.cliproxyPluginCall)
	plugin.free_buffer = C.cliproxy_plugin_free_fn(C.cliproxyPluginFree)
	plugin.shutdown = C.cliproxy_plugin_shutdown_fn(C.cliproxyPluginShutdown)
	return 0
}

//export cliproxyPluginCall
func cliproxyPluginCall(method *C.char, request *C.uint8_t, requestLen C.size_t, response *C.cliproxy_buffer) (code C.int) {
	if response == nil {
		return 1
	}
	response.ptr = nil
	response.len = 0
	defer func() {
		if recover() != nil {
			writeResponse(response, []byte(`{"ok":false,"error":{"code":"plugin_error","message":"插件内部错误"}}`))
			code = 1
		}
	}()
	if method == nil || requestLen > 8*1024*1024 || (requestLen > 0 && request == nil) {
		return 1
	}
	appMu.RLock()
	current := app
	appMu.RUnlock()
	if current == nil {
		return 1
	}
	var raw []byte
	if requestLen > 0 {
		raw = C.GoBytes(unsafe.Pointer(request), C.int(requestLen))
	}
	writeResponse(response, current.Call(C.GoString(method), raw))
	return 0
}

//export cliproxyPluginFree
func cliproxyPluginFree(ptr unsafe.Pointer, length C.size_t) {
	if ptr != nil {
		C.free(ptr)
	}
}

//export cliproxyPluginShutdown
func cliproxyPluginShutdown() {
	appMu.RLock()
	current := app
	appMu.RUnlock()
	if current != nil {
		current.Engine.Close()
	}
}

func writeResponse(response *C.cliproxy_buffer, raw []byte) {
	if response == nil || len(raw) == 0 {
		return
	}
	response.ptr = C.CBytes(raw)
	response.len = C.size_t(len(raw))
}

type callbackHost struct{}

func (callbackHost) Call(method string, request any, out any) error {
	raw, err := json.Marshal(request)
	if err != nil {
		return errors.New("无法编码宿主请求")
	}
	cMethod := C.CString(method)
	defer C.free(unsafe.Pointer(cMethod))
	data := C.CBytes(raw)
	defer C.free(data)
	var response C.cliproxy_buffer
	code := C.call_host_api(cMethod, (*C.uint8_t)(data), C.size_t(len(raw)), &response)
	if response.ptr != nil {
		defer C.free_host_buffer(response.ptr, response.len)
	}
	if response.ptr == nil || response.len == 0 || response.len > 16*1024*1024 {
		return errors.New("CPA 宿主响应无效")
	}
	reply := C.GoBytes(response.ptr, C.int(response.len))
	var env struct {
		OK     bool            `json:"ok"`
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code       string `json:"code"`
			StatusCode int    `json:"http_status"`
		} `json:"error"`
	}
	if json.Unmarshal(reply, &env) != nil {
		return errors.New("CPA 宿主响应格式无效")
	}
	if !env.OK {
		failure := &wa.HostError{}
		if env.Error != nil {
			failure.Code = env.Error.Code
			failure.StatusCode = env.Error.StatusCode
		}
		return failure
	}
	if code != 0 {
		return errors.New("CPA 宿主调用失败")
	}
	if out != nil && json.Unmarshal(env.Result, out) != nil {
		return errors.New("CPA 宿主结果格式无效")
	}
	return nil
}
