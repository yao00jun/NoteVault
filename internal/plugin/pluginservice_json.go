package plugin

import "encoding/json"

// jsonUnmarshalInto 解析 JSON 到目标值。
// 用泛型而非 map[string]bool 专用版本，是为了让启用状态（map[string]bool）
// 与信任授权（map[string]trustRecord）共用同一套封装。
func jsonUnmarshalInto[T any](data []byte, target *T) error {
	return json.Unmarshal(data, target)
}

// jsonMarshalValue 序列化任意值为 JSON
func jsonMarshalValue[T any](v T) ([]byte, error) {
	return json.Marshal(v)
}
