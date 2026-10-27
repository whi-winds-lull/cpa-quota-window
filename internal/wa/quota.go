package wa

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// FiveHourQuota 只在能确认是 300 分钟窗口时才返回重置时间，避免误报。
func FiveHourQuota(headers http.Header, observed time.Time) (*time.Time, *float64) {
	values := map[string]string{}
	for key, raw := range headers {
		if len(raw) > 0 {
			values[strings.ToLower(key)] = raw[0]
		}
	}
	for _, bucket := range []string{"primary", "secondary"} {
		prefix := "x-codex-" + bucket + "-"
		minutes, err := strconv.Atoi(values[prefix+"window-minutes"])
		if err != nil || minutes != 300 {
			continue
		}
		var reset *time.Time
		value := values[prefix+"reset-at"]
		if seconds, err := strconv.ParseInt(value, 10, 64); err == nil && seconds > 0 {
			at := time.Unix(seconds, 0).UTC()
			reset = &at
		} else if at, err := time.Parse(time.RFC3339, value); err == nil {
			utc := at.UTC()
			reset = &utc
		}
		if reset == nil {
			if seconds, err := strconv.ParseFloat(values[prefix+"reset-after-seconds"], 64); err == nil && seconds >= 0 && seconds <= 18060 {
				at := observed.Add(time.Duration(seconds * float64(time.Second))).UTC()
				reset = &at
			}
		}
		var used *float64
		if percent, err := strconv.ParseFloat(values[prefix+"used-percent"], 64); err == nil && percent >= 0 && percent <= 100 {
			used = &percent
		}
		return reset, used
	}
	return nil, nil
}
