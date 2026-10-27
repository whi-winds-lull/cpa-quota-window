package wa

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// diskState 记录"哪些时刻已经发过"（防重启重复发送）和最近的执行结果。
type diskState struct {
	Claims  map[string]time.Time `json:"claims"`
	Results []Result             `json:"results"`
}

func readState(path string) (diskState, error) {
	st := diskState{Claims: map[string]time.Time{}}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return st, nil
		}
		return st, err
	}
	if len(raw) == 0 {
		return st, nil
	}
	if err := json.Unmarshal(raw, &st); err != nil {
		return diskState{Claims: map[string]time.Time{}}, err
	}
	if st.Claims == nil {
		st.Claims = map[string]time.Time{}
	}
	return st, nil
}

func writeState(path string, st diskState) error {
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
