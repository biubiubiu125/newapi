package common

import (
	"bytes"
	"encoding/json"
	"io"
)

// 本包是业务代码唯一允许的 JSON 编解码入口。
//
// 不变式：图片任务请求指纹（newapi-image-task-v1，见 controller/image_task.go）依赖
// Marshal 对 map 键排序的确定性。如果将来把实现换成不保证键序的库，必须为指纹保留
// 标准库路径，或把指纹前缀升到 v2 并按版本前缀隔离比较逻辑，否则同内容重试会被误判为
// 幂等键冲突（409）。
func Unmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

func UnmarshalJsonStr(data string, v any) error {
	return json.Unmarshal(StringToByteSlice(data), v)
}

func DecodeJson(reader io.Reader, v any) error {
	return json.NewDecoder(reader).Decode(v)
}

// DecodeJsonUseNumber 以 json.Number 解析数字，避免浮点归一化改变原始字面量。
func DecodeJsonUseNumber(reader io.Reader, v any) error {
	decoder := json.NewDecoder(reader)
	decoder.UseNumber()
	return decoder.Decode(v)
}

func Marshal(v any) ([]byte, error) {
	return json.Marshal(v)
}

// JsonValid 判断字节切片是否为合法 JSON。
func JsonValid(data []byte) bool {
	return json.Valid(data)
}

func GetJsonType(data json.RawMessage) string {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return "unknown"
	}
	firstChar := trimmed[0]
	switch firstChar {
	case '{':
		return "object"
	case '[':
		return "array"
	case '"':
		return "string"
	case 't', 'f':
		return "boolean"
	case 'n':
		return "null"
	default:
		return "number"
	}
}

// JsonRawMessageToString returns JSON strings as their decoded value and other JSON values as raw text.
func JsonRawMessageToString(data json.RawMessage) string {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return ""
	}
	if trimmed[0] != '"' {
		return string(trimmed)
	}
	var value string
	if err := Unmarshal(trimmed, &value); err != nil {
		return string(trimmed)
	}
	return value
}
