package common

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"

	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

// 本包是业务代码唯一允许的 JSON 编解码入口。
//
// 不变式：图片任务请求指纹（newapi-image-task-v1，见 controller/image_task.go）依赖
// Marshal 对 map 键排序的确定性。hostJSONCodec 必须继续走标准库 encoding/json；
// 如果将来把实现换成不保证键序的库，必须为指纹保留标准库路径，或把指纹前缀升到 v2
// 并按版本前缀隔离比较逻辑，否则同内容重试会被误判为幂等键冲突（409）。

// hostJSONCodec is the single place where the host chooses its JSON engine.
// Swap the implementation here and every common.* and kitutil.* JSON helper,
// including relaykit DTO (un)marshalling, follows. Injected from init() rather
// than main() so tests run on the same engine as production.
type hostJSONCodec struct{}

func (hostJSONCodec) Marshal(v any) ([]byte, error) {
	return json.Marshal(v)
}

func (hostJSONCodec) Unmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

func (hostJSONCodec) Decode(r io.Reader, v any) error {
	return json.NewDecoder(r).Decode(v)
}

func (hostJSONCodec) Valid(data []byte) bool {
	return json.Valid(data)
}

func init() {
	kitutil.SetCodec(hostJSONCodec{})
}

func Unmarshal(data []byte, v any) error {
	return kitutil.Unmarshal(data, v)
}

func UnmarshalJsonStr(data string, v any) error {
	return kitutil.UnmarshalJsonStr(data, v)
}

func DecodeJson(reader io.Reader, v any) error {
	return decodeSingleJSON(json.NewDecoder(reader), v)
}

// DecodeJsonUseNumber 以 json.Number 解析数字，避免浮点归一化改变原始字面量。
func DecodeJsonUseNumber(reader io.Reader, v any) error {
	decoder := json.NewDecoder(reader)
	decoder.UseNumber()
	return decodeSingleJSON(decoder, v)
}

func decodeSingleJSON(decoder *json.Decoder, v any) error {
	if err := decoder.Decode(v); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return errors.New("multiple JSON values are not allowed")
		}
		return err
	}
	return nil
}

func Marshal(v any) ([]byte, error) {
	return kitutil.Marshal(v)
}

func IndentJson(data []byte) ([]byte, error) {
	var buffer bytes.Buffer
	if err := json.Indent(&buffer, data, "", "  "); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// JsonValid 判断字节切片是否为合法 JSON。
func JsonValid(data []byte) bool {
	return kitutil.Valid(data)
}

func GetJsonType(data json.RawMessage) string {
	return kitutil.GetJsonType(data)
}

// JsonRawMessageToString returns JSON strings as their decoded value and other JSON values as raw text.
func JsonRawMessageToString(data json.RawMessage) string {
	return kitutil.JsonRawMessageToString(data)
}
