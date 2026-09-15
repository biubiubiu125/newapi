package jsplugin

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"net/url"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	pluginruntime "github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const mockPlugin = `
export const meta = {
  apiVersion: 1, key: "mock-task", name: "Mock Task", version: "1.0.0",
  author: {name: "Test"},
  channelTypes: [1001], models: ["mock-v1"], fetchMode: "per_task",
  protocols: ["openai_video"],
  usageSchema: {seconds: {type: "number", unit: "second"}, mode: {enum: ["std", "pro"]}},
};
export function buildSubmitRequest(ctx) {
  if (!ctx.requestBody.prompt) throw new Error("prompt required");
  return { url: ctx.baseUrl + "/submit", method: "POST", headers: {"X-Plugin": "submit"}, body: {prompt: ctx.requestBody.prompt}, action: "text_to_video", model: "mock-v1", rewriteModel: "mock-upstream" };
}

export function parseSubmitResponse(ctx, resp) {
  return {
    taskId: resp.body.id,
    taskData: {accepted: true, status: resp.statusCode},
  };
}
export function extractUsage(ctx) { return {seconds: 5, mode: "pro"}; }
export function extractUsageOnSubmit(ctx, data) { return {seconds: data.seconds || 7}; }
export function extractUsageOnComplete(task, result) { return {upstreamUnits: 23}; }
export function buildQueryRequest(ctx) { return {url: ctx.baseUrl + "/tasks/" + ctx.taskId, method: "GET", headers: {"X-Plugin": "query"}}; }
export function parseTaskResult(ctx, body) { return {taskId: body.id, status: "SUCCESS", progress: "100%", url: body.url}; }
export function listArtifacts() { return []; }
export function buildContentRequest() { throw new Error("artifact_not_found"); }
export const protocols = {openai_video: {
  decodeRequest: function(ctx) { return {kind: "submit", model: ctx.model, requestBody: ctx.body.value}; },
  render: function(ctx, task) { return {id: task.task_id, status: "completed"}; }
}};
`

func TestTaskAdaptorRejectsDeprecatedClientResponse(t *testing.T) {
	source := strings.Replace(mockPlugin, `taskData: {accepted: true, status: resp.statusCode},`, `taskData: {}, clientResponse: {id: ctx.publicTaskId},`, 1)
	plugin, err := pluginruntime.NewRegistry().Register(source, pluginruntime.Options{})
	require.NoError(t, err)
	adaptor := New(plugin)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}, TaskRelayInfo: &relaycommon.TaskRelayInfo{PublicTaskID: "task_public"}}
	adaptor.Init(info)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	response := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"id":"upstream"}`))}

	parsed, taskErr := adaptor.ParseResponse(c, response, info)

	assert.Nil(t, parsed)
	require.NotNil(t, taskErr)
	require.Error(t, taskErr.Error)
	assert.Contains(t, taskErr.Error.Error(), "must not return clientResponse")
}

func TestTaskAdaptorBuildsMultipartFromOpaqueFileReference(t *testing.T) {
	source := `
export const meta = {apiVersion:1,key:"multipart",name:"Multipart",version:"1.0.0",author:{name:"Test"},models:["m"],fetchMode:"per_task"};
export function buildSubmitRequest(ctx) { return {url:ctx.baseUrl+"/submit",bodyType:"multipart",parts:[{name:"model",value:"m"},{name:"input_reference",fileRef:ctx.files[0].ref}]}; }
export function parseSubmitResponse(ctx,r){return {taskId:"1"}} export function buildQueryRequest(){return {url:"https://example.com"}} export function parseTaskResult(){return {status:"SUCCESS"}}
`
	plugin, err := pluginruntime.NewRegistry().Register(source, pluginruntime.Options{})
	require.NoError(t, err)
	adaptor := New(plugin)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: "https://provider.example"}, TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
	adaptor.Init(info)
	var input bytes.Buffer
	writer := multipart.NewWriter(&input)
	file, err := writer.CreateFormFile("input_reference", "ref.png")
	require.NoError(t, err)
	_, err = file.Write([]byte("image-bytes"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader(input.Bytes()))
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	c.Set("task_request", relaycommon.TaskSubmitReq{Prompt: "p"})
	body, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	requestBytes, err := io.ReadAll(body)
	require.NoError(t, err)
	reader := multipart.NewReader(bytes.NewReader(requestBytes), strings.TrimPrefix(c.GetHeader("Content-Type"), "multipart/form-data; boundary="))
	form, err := reader.ReadForm(1024)
	require.NoError(t, err)
	assert.Equal(t, []string{"m"}, form.Value["model"])
	require.Len(t, form.File["input_reference"], 1)
	opened, err := form.File["input_reference"][0].Open()
	require.NoError(t, err)
	content, err := io.ReadAll(opened)
	require.NoError(t, err)
	assert.Equal(t, "image-bytes", string(content))
}

func TestTaskAdaptorInlinesJSONFilePlaceholders(t *testing.T) {
	const fileBytes = "image-bytes"
	encoded := base64.StdEncoding.EncodeToString([]byte(fileBytes))
	source := `
export const meta = {apiVersion:1,key:"json-inline",name:"JSON Inline",version:"1.0.0",author:{name:"Test"},models:["m"],fetchMode:"per_task"};
export function buildSubmitRequest(ctx) {
  return {url:ctx.baseUrl+"/submit",body:{
    prompt:"p",
    image:{__fileRef:ctx.files[0].ref,encoding:"base64"},
    nested:{items:[{__fileRef:ctx.files[0].ref,encoding:"dataUrl",mimeType:"image/png"}]},
    dataUrl:{__fileRef:ctx.files[0].ref,encoding:"dataUrl"}
  }};
}
export function parseSubmitResponse(){return {taskId:"1"}} export function buildQueryRequest(){return {url:"https://example.com"}} export function parseTaskResult(){return {status:"SUCCESS"}}
`
	plugin, err := pluginruntime.NewRegistry().Register(source, pluginruntime.Options{})
	require.NoError(t, err)
	adaptor := New(plugin)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: "https://provider.example"}, TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
	adaptor.Init(info)
	c := newMultipartFileContext(t, "input_reference", "ref.png", "image/jpeg", []byte(fileBytes))
	c.Set("task_request", map[string]any{"prompt": "p"})
	body, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	requestBytes, err := io.ReadAll(body)
	require.NoError(t, err)
	var decoded map[string]any
	require.NoError(t, common.Unmarshal(requestBytes, &decoded))
	assert.Equal(t, "p", decoded["prompt"])
	assert.Equal(t, encoded, decoded["image"])
	nested := decoded["nested"].(map[string]any)
	items := nested["items"].([]any)
	require.Len(t, items, 1)
	assert.Equal(t, "data:image/png;base64,"+encoded, items[0])
	assert.Equal(t, "data:image/jpeg;base64,"+encoded, decoded["dataUrl"])
}

func TestTaskAdaptorJSONFilePlaceholderErrors(t *testing.T) {
	tests := []struct {
		name        string
		part        string
		fileSize    int
		globalMB    int
		wantContain string
	}{
		{name: "unknown ref", part: `{__fileRef:"request_file:missing",encoding:"base64"}`, fileSize: 4, wantContain: `unknown file reference "request_file:missing"`},
		{name: "extra key", part: `{__fileRef:"request_file:input_reference",encoding:"base64",extra:true}`, fileSize: 4, wantContain: "invalid file placeholder"},
		{name: "missing encoding", part: `{__fileRef:"request_file:input_reference"}`, fileSize: 4, wantContain: "encoding"},
		{name: "oversize maxBytes", part: `{__fileRef:"request_file:input_reference",encoding:"base64",maxBytes:3}`, fileSize: 4, wantContain: "3 byte limit"},
		{name: "oversize global", part: `{__fileRef:"request_file:input_reference",encoding:"base64"}`, fileSize: 2 << 20, globalMB: 1, wantContain: "1048576 byte limit"},
		{name: "multiple references cap", part: `{a:{__fileRef:"request_file:input_reference",encoding:"base64"},b:{__fileRef:"request_file:input_reference",encoding:"base64"}}`, fileSize: 700 << 10, globalMB: 1, wantContain: "1048576 byte limit"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			if testCase.globalMB > 0 {
				previous := constant.MaxFileDownloadMB
				constant.MaxFileDownloadMB = testCase.globalMB
				t.Cleanup(func() { constant.MaxFileDownloadMB = previous })
			}
			source := strings.Replace(`
export const meta = {apiVersion:1,key:"json-inline-err",name:"JSON Inline Err",version:"1.0.0",author:{name:"Test"},models:["m"],fetchMode:"per_task"};
export function buildSubmitRequest() { return {url:"https://provider.example/submit",body:PLACEHOLDER}; }
export function parseSubmitResponse(){return {taskId:"1"}} export function buildQueryRequest(){return {url:"https://example.com"}} export function parseTaskResult(){return {status:"SUCCESS"}}
`, "PLACEHOLDER", testCase.part, 1)
			plugin, err := pluginruntime.NewRegistry().Register(source, pluginruntime.Options{})
			require.NoError(t, err)
			adaptor := New(plugin)
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: "https://provider.example"}, TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
			adaptor.Init(info)
			c := newMultipartFileContext(t, "input_reference", "ref.bin", "application/octet-stream", bytes.Repeat([]byte("x"), testCase.fileSize))
			c.Set("task_request", map[string]any{"prompt": "p"})
			_, err = adaptor.BuildRequestBody(c, info)
			require.Error(t, err)
			assert.Contains(t, err.Error(), testCase.wantContain)
		})
	}
}

func newMultipartFileContext(t *testing.T, field, filename, contentType string, content []byte) *gin.Context {
	t.Helper()
	var input bytes.Buffer
	writer := multipart.NewWriter(&input)
	part, err := writer.CreatePart(textproto.MIMEHeader{
		"Content-Disposition": {`form-data; name="` + field + `"; filename="` + filename + `"`},
		"Content-Type":        {contentType},
	})
	require.NoError(t, err)
	_, err = part.Write(content)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader(input.Bytes()))
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	return c
}

func TestTaskAdaptorDoesNotEmitInjectedMultipartDispositionHeaders(t *testing.T) {
	tests := []struct {
		name string
		part string
	}{
		{name: "part name", part: `{name:"prompt\r\nX-Injected: yes",value:"hello"}`},
		{name: "filename", part: `{name:"input_reference",fileRef:"request_file:input_reference",filename:"safe.png\r\nX-Injected: yes"}`},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			source := strings.Replace(`
export const meta = {apiVersion:1,key:"multipart-safe",name:"Multipart Safe",version:"1.0.0",author:{name:"Test"},models:["m"],fetchMode:"per_task"};
export function buildSubmitRequest(ctx) { return {url:ctx.baseUrl+"/submit",bodyType:"multipart",parts:[PART]}; }
export function parseSubmitResponse(){return {taskId:"1"}} export function buildQueryRequest(){return {}} export function parseTaskResult(){return {status:"SUCCESS"}}
`, "PART", testCase.part, 1)
			plugin, err := pluginruntime.NewRegistry().Register(source, pluginruntime.Options{})
			require.NoError(t, err)
			adaptor := New(plugin)
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: "https://provider.example"}, TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
			adaptor.Init(info)
			var input bytes.Buffer
			writer := multipart.NewWriter(&input)
			file, err := writer.CreateFormFile("input_reference", "input.png")
			require.NoError(t, err)
			_, err = file.Write([]byte("image"))
			require.NoError(t, err)
			require.NoError(t, writer.Close())
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader(input.Bytes()))
			c.Request.Header.Set("Content-Type", writer.FormDataContentType())
			c.Set("task_request", map[string]any{"model": "m"})

			body, buildErr := adaptor.BuildRequestBody(c, info)
			if buildErr != nil {
				assert.Contains(t, buildErr.Error(), "multipart")
				return
			}
			encoded, err := io.ReadAll(body)
			require.NoError(t, err)
			assert.NotContains(t, string(encoded), "\r\nX-Injected: yes")
		})
	}
}

func TestTaskAdaptorRejectsPostDistributionEndpointModelDrift(t *testing.T) {
	source := `
export const meta = {apiVersion:1,key:"endpoint-drift",name:"Endpoint Drift",version:"1.0.0",author:{name:"Test"},models:["claimed-model"],fetchMode:"per_task"};
export function buildSubmitRequest(ctx) {
  return {url:ctx.baseUrl+"/submit",method:"POST",model:"outside-model",rewriteModel:"allowed-upstream-rewrite"};
}

export function parseSubmitResponse(){return {taskId:"1"}}
export function buildQueryRequest(){return {url:"https://example.com"}}
export function parseTaskResult(){return {status:"SUCCESS"}}
`
	plugin, err := pluginruntime.NewRegistry().Register(source, pluginruntime.Options{})
	require.NoError(t, err)
	adaptor := New(plugin)
	info := &relaycommon.RelayInfo{
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelBaseUrl: "https://provider.example"},
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
		OriginModelName: "claimed-model",
	}
	adaptor.Init(info)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set("task_request", map[string]any{"model": "claimed-model"})
	c.Set("resolved_task_model", "claimed-model")
	c.Set(pluginruntime.ContextKeyPinnedEndpoint, pluginruntime.PinnedEndpoint{
		Plugin: plugin,
	})

	taskErr := adaptor.ValidateRequestAndSetAction(c, info)

	require.NotNil(t, taskErr)
	assert.Equal(t, "claimed-model", info.OriginModelName)
	assert.Contains(t, taskErr.Message, "does not match")
}

func TestTaskAdaptorReDecodesFinalCandidateAndRejectsModelDrift(t *testing.T) {
	source := `
export const meta = {apiVersion:1,key:"redecode",name:"Redecode",version:"1.0.0",author:{name:"Test"},models:["claimed-model"],fetchMode:"per_task",protocols:[{name:"openai_responses",supports:["sync","background"]}]};
export const protocols = {openai_responses:{decodeRequest:function(ctx){return {kind:"submit",model:"drifted-model",requestBody:ctx.body.value};},renderFinal:function(){return {};}}};
export function buildSubmitRequest(ctx){return {url:ctx.baseUrl+"/submit"}} export function parseSubmitResponse(){return {taskId:"one"}} export function buildQueryRequest(){return {}} export function parseTaskResult(){return {status:"SUCCESS"}}
`
	plugin, err := pluginruntime.NewRegistry().Register(source, pluginruntime.Options{})
	require.NoError(t, err)
	protocolContext := pluginruntime.ProtocolRequestContext{
		RouteRequestContext: pluginruntime.RouteRequestContext{Body: map[string]any{"kind": "json", "value": map[string]any{"model": "claimed-model"}}, RequestBody: map[string]any{"model": "claimed-model"}},
		Protocol:            "openai_responses", Model: "claimed-model",
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set(pluginruntime.ContextKeyPinnedEndpoint, pluginruntime.PinnedEndpoint{Plugin: plugin, Protocol: "openai_responses", Model: "claimed-model"})
	c.Set(pluginruntime.ContextKeyProtocolRequest, protocolContext)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: "https://provider.example"}, TaskRelayInfo: &relaycommon.TaskRelayInfo{}, OriginModelName: "claimed-model"}
	adaptor := New(plugin)
	adaptor.Init(info)

	taskErr := adaptor.ValidateRequestAndSetAction(c, info)

	require.NotNil(t, taskErr)
	assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
	assert.Contains(t, taskErr.Message, "pinned model")
}

func TestTaskAdaptorReDecodeRejectsNonSubmitKind(t *testing.T) {
	source := `
export const meta = {apiVersion:1,key:"redecode-kind",name:"Redecode Kind",version:"1.0.0",author:{name:"Test"},models:["claimed-model"],fetchMode:"per_task",protocols:[{name:"openai_responses",supports:["sync","background"]}]};
export const protocols = {openai_responses:{decodeRequest:function(ctx){return {kind:"query",model:ctx.model,taskIds:["task-1"]};},renderFinal:function(){return {};}}};
export function buildSubmitRequest(ctx){return {url:ctx.baseUrl+"/submit"}}
export function parseSubmitResponse(){return {taskId:"one"}}
export function buildQueryRequest(){return {}}
export function parseTaskResult(){return {status:"SUCCESS"}}
`
	plugin, err := pluginruntime.NewRegistry().Register(source, pluginruntime.Options{})
	require.NoError(t, err)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set(pluginruntime.ContextKeyPinnedEndpoint, pluginruntime.PinnedEndpoint{
		Plugin: plugin, Protocol: "openai_responses", Model: "claimed-model",
	})
	c.Set(pluginruntime.ContextKeyProtocolRequest, pluginruntime.ProtocolRequestContext{
		RouteRequestContext: pluginruntime.RouteRequestContext{
			Body: map[string]any{
				"kind":  "json",
				"value": map[string]any{"model": "claimed-model"},
			},
		},
		Protocol: "openai_responses",
		Model:    "claimed-model",
	})
	c.Set("task_request", map[string]any{"model": "claimed-model"})
	info := &relaycommon.RelayInfo{
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelBaseUrl: "https://provider.example"},
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
		OriginModelName: "claimed-model",
	}
	adaptor := New(plugin)
	adaptor.Init(info)

	taskErr := adaptor.ValidateRequestAndSetAction(c, info)

	require.NotNil(t, taskErr)
	assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
	assert.Contains(t, taskErr.Message, "submit")
}

func TestTaskAdaptorReDecodeRejectsInvalidAction(t *testing.T) {
	source := `
export const meta = {apiVersion:1,key:"redecode-action",name:"Redecode Action",version:"1.0.0",author:{name:"Test"},models:["claimed-model"],fetchMode:"per_task",protocols:[{name:"openai_responses",supports:["sync","background"]}]};
export const protocols = {openai_responses:{decodeRequest:function(ctx){return {kind:"submit",model:ctx.model,action:123,requestBody:ctx.body.value};},renderFinal:function(){return {};}}};
export function buildSubmitRequest(ctx){return {url:ctx.baseUrl+"/submit"}}
export function parseSubmitResponse(){return {taskId:"one"}}
export function buildQueryRequest(){return {}}
export function parseTaskResult(){return {status:"SUCCESS"}}
`
	plugin, err := pluginruntime.NewRegistry().Register(source, pluginruntime.Options{})
	require.NoError(t, err)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set(pluginruntime.ContextKeyPinnedEndpoint, pluginruntime.PinnedEndpoint{
		Plugin: plugin, Protocol: "openai_responses", Model: "claimed-model",
	})
	c.Set(pluginruntime.ContextKeyProtocolRequest, pluginruntime.ProtocolRequestContext{
		RouteRequestContext: pluginruntime.RouteRequestContext{
			Body: map[string]any{
				"kind":  "json",
				"value": map[string]any{"model": "claimed-model"},
			},
		},
		Protocol: "openai_responses",
		Model:    "claimed-model",
	})
	info := &relaycommon.RelayInfo{
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelBaseUrl: "https://provider.example"},
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
		OriginModelName: "claimed-model",
	}
	adaptor := New(plugin)
	adaptor.Init(info)

	taskErr := adaptor.ValidateRequestAndSetAction(c, info)

	require.NotNil(t, taskErr)
	assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
	assert.Contains(t, taskErr.Message, "action")
}

func TestTaskAdaptorReDecodeClearsStaleRequestBodyWhenFinalDecoderOmitsIt(t *testing.T) {
	source := `
export const meta = {apiVersion:1,key:"redecode-body",name:"Redecode Body",version:"1.0.0",author:{name:"Test"},models:["claimed-model"],fetchMode:"per_task",protocols:[{name:"openai_responses",supports:["sync","background"]}]};
export const protocols = {openai_responses:{decodeRequest:function(ctx){return {kind:"submit",model:ctx.model};},renderFinal:function(){return {};}}};
export function buildSubmitRequest(ctx){
  if (ctx.requestBody && ctx.requestBody.fromFirst) throw new Error("stale request body leaked");
  if (!ctx.requestBody || ctx.requestBody.prompt !== "fresh") throw new Error("fresh request body missing");
  return {url:ctx.baseUrl+"/submit"};
}
export function parseSubmitResponse(){return {taskId:"one"}}
export function buildQueryRequest(){return {}}
export function parseTaskResult(){return {status:"SUCCESS"}}
`
	plugin, err := pluginruntime.NewRegistry().Register(source, pluginruntime.Options{})
	require.NoError(t, err)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(
		http.MethodPost,
		"/v1/responses",
		strings.NewReader(`{"model":"claimed-model","prompt":"fresh"}`),
	)
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(pluginruntime.ContextKeyPinnedEndpoint, pluginruntime.PinnedEndpoint{
		Plugin: plugin, Protocol: "openai_responses", Model: "claimed-model",
	})
	c.Set(pluginruntime.ContextKeyProtocolRequest, pluginruntime.ProtocolRequestContext{
		RouteRequestContext: pluginruntime.RouteRequestContext{
			Body: map[string]any{
				"kind":  "json",
				"value": map[string]any{"model": "claimed-model", "prompt": "fresh"},
			},
		},
		Protocol: "openai_responses",
		Model:    "claimed-model",
	})
	c.Set(pluginruntime.ContextKeyRouteRequest, pluginruntime.RouteRequestContext{
		Body: map[string]any{
			"kind":  "json",
			"value": map[string]any{"model": "claimed-model", "prompt": "fresh"},
		},
		RequestBody: map[string]any{"fromFirst": true},
	})
	c.Set("task_request", map[string]any{"fromFirst": true})
	info := &relaycommon.RelayInfo{
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelBaseUrl: "https://provider.example"},
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
		OriginModelName: "claimed-model",
	}
	adaptor := New(plugin)
	adaptor.Init(info)

	taskErr := adaptor.ValidateRequestAndSetAction(c, info)

	require.Nil(t, taskErr)
}

func TestTaskAdaptorReDecodeClearsStaleActionWhenFinalDecoderOmitsIt(t *testing.T) {
	source := `
export const meta = {apiVersion:1,key:"redecode-action-clear",name:"Redecode Action Clear",version:"1.0.0",author:{name:"Test"},models:["claimed-model"],fetchMode:"per_task",protocols:[{name:"openai_responses",supports:["sync","background"]}]};
export const protocols = {openai_responses:{decodeRequest:function(ctx){return {kind:"submit",model:ctx.model,requestBody:ctx.body.value};},renderFinal:function(){return {};}}};
export function buildSubmitRequest(ctx){
  if (ctx.action === "first-action") throw new Error("stale action leaked");
  return {url:ctx.baseUrl+"/submit"};
}
export function parseSubmitResponse(){return {taskId:"one"}}
export function buildQueryRequest(){return {}}
export function parseTaskResult(){return {status:"SUCCESS"}}
`
	plugin, err := pluginruntime.NewRegistry().Register(source, pluginruntime.Options{})
	require.NoError(t, err)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set(pluginruntime.ContextKeyPinnedEndpoint, pluginruntime.PinnedEndpoint{
		Plugin: plugin, Protocol: "openai_responses", Model: "claimed-model",
	})
	c.Set(pluginruntime.ContextKeyProtocolRequest, pluginruntime.ProtocolRequestContext{
		RouteRequestContext: pluginruntime.RouteRequestContext{
			Body: map[string]any{
				"kind":  "json",
				"value": map[string]any{"model": "claimed-model"},
			},
		},
		Protocol: "openai_responses",
		Model:    "claimed-model",
	})
	c.Set("task_request", map[string]any{"model": "claimed-model"})
	c.Set("task_action", "first-action")
	info := &relaycommon.RelayInfo{
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelBaseUrl: "https://provider.example"},
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{Action: "first-action"},
		OriginModelName: "claimed-model",
	}
	adaptor := New(plugin)
	adaptor.Init(info)

	taskErr := adaptor.ValidateRequestAndSetAction(c, info)

	require.Nil(t, taskErr)
	assert.Empty(t, c.GetString("task_action"))
	assert.Empty(t, info.Action)
}

func TestTaskAdaptorRejectsRendererFromFinalProtocolDecoder(t *testing.T) {
	source := `
export const meta = {apiVersion:1,key:"renderer-reject",name:"Renderer Reject",version:"1.0.0",author:{name:"Test"},models:["claimed-model"],fetchMode:"per_task",protocols:[{name:"openai_responses",supports:["sync","background"]}]};
export const protocols = {openai_responses:{decodeRequest:function(ctx){return {kind:"submit",model:ctx.model,requestBody:ctx.body.value,renderer:"legacy"};},renderFinal:function(){return {};}}};
export function buildSubmitRequest(ctx){return {url:ctx.baseUrl+"/submit"}} export function parseSubmitResponse(){return {taskId:"one"}} export function buildQueryRequest(){return {}} export function parseTaskResult(){return {status:"SUCCESS"}}
`
	plugin, err := pluginruntime.NewRegistry().Register(source, pluginruntime.Options{})
	require.NoError(t, err)
	protocolContext := pluginruntime.ProtocolRequestContext{
		RouteRequestContext: pluginruntime.RouteRequestContext{Body: map[string]any{"kind": "json", "value": map[string]any{"model": "claimed-model"}}},
		Protocol:            "openai_responses", Model: "claimed-model",
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set(pluginruntime.ContextKeyPinnedEndpoint, pluginruntime.PinnedEndpoint{Plugin: plugin, Protocol: "openai_responses", Model: "claimed-model"})
	c.Set(pluginruntime.ContextKeyProtocolRequest, protocolContext)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: "https://provider.example"}, TaskRelayInfo: &relaycommon.TaskRelayInfo{}, OriginModelName: "claimed-model"}
	adaptor := New(plugin)
	adaptor.Init(info)

	taskErr := adaptor.ValidateRequestAndSetAction(c, info)

	require.NotNil(t, taskErr)
	assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
	assert.Contains(t, taskErr.Message, "must not return renderer")
}

func TestTaskAdaptorBuildContentRequestHookAndMissingFallback(t *testing.T) {
	source := strings.Replace(mockPlugin, `export function listArtifacts() { return []; }
export function buildContentRequest() { throw new Error("artifact_not_found"); }`, `export function listArtifacts(task) { return [{key: "video", type: "video", mimeType: "video/mp4"}]; }
export function buildContentRequest(ctx) {
  if (ctx.data.id !== "raw-upstream" || ctx.upstreamTaskId !== "upstream-task" || ctx.producerVersion !== "0.9.0") throw new Error("bad task context");
  return {url: ctx.baseUrl + "/content/" + ctx.artifactKey, method: ctx.clientRequest.method, headers: {"X-Content": "plugin"}};
}`, 1)
	plugin, err := pluginruntime.NewRegistry().Register(source, pluginruntime.Options{})
	require.NoError(t, err)
	adaptor := New(plugin)
	adaptor.Init(&relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: "https://provider.example", ApiKey: "key"}})
	taskData, err := common.Marshal(map[string]any{"id": "raw-upstream"})
	require.NoError(t, err)
	task := &model.Task{
		TaskID: "task-public", Status: model.TaskStatusSuccess, Data: taskData,
		PrivateData: model.TaskPrivateData{
			UpstreamTaskID: "upstream-task",
			Execution: &model.TaskExecutionSnapshot{TaskPlugin: &model.TaskPluginSnapshot{
				Version: "0.9.0",
			}},
		},
	}
	artifacts, err := adaptor.ListArtifacts(task)
	require.NoError(t, err)
	require.Equal(t, []channel.TaskArtifact{{Key: "video", Type: "video", MimeType: "video/mp4"}}, artifacts)
	descriptor, err := adaptor.BuildContentRequest(task, "video", channel.TaskArtifactClientRequest{Method: http.MethodHead})
	require.NoError(t, err)
	require.NotNil(t, descriptor)
	assert.Equal(t, "https://provider.example/content/video", descriptor.URL)
	assert.Equal(t, http.MethodHead, descriptor.Method)
	assert.Equal(t, "plugin", descriptor.Headers["X-Content"])

	withoutHook, err := pluginruntime.NewRegistry().Register(`
export const meta = {apiVersion:1,key:"no-artifacts",name:"No Artifacts",version:"1.0.0",author:{name:"Test"},models:["m"],fetchMode:"per_task"};
export function buildSubmitRequest(){return {url:"https://provider.example"};}
export function parseSubmitResponse(){return {taskId:"1"};}
export function buildQueryRequest(){return {url:"https://provider.example"};}
export function parseTaskResult(){return {status:"SUCCESS"};}
`, pluginruntime.Options{})
	require.NoError(t, err)
	fallback := New(withoutHook)
	fallback.Init(&relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: "https://provider.example"}})
	artifacts, err = fallback.ListArtifacts(&model.Task{})
	require.NoError(t, err)
	assert.Nil(t, artifacts)
	descriptor, err = fallback.BuildContentRequest(&model.Task{}, "video", channel.TaskArtifactClientRequest{Method: http.MethodGet})
	require.NoError(t, err)
	assert.Nil(t, descriptor)
}

func TestTaskAdaptorRejectsInvalidArtifactProjection(t *testing.T) {
	testCases := []struct {
		name       string
		projection string
	}{
		{name: "duplicate key", projection: `[{key:"video",type:"video"},{key:"video",type:"video"}]`},
		{name: "array index identity", projection: `[{key:"video",type:"video",index:0}]`},
		{name: "upstream url", projection: `[{key:"video",type:"video",url:"https://cdn.example/video.mp4"}]`},
		{name: "invalid key", projection: `[{key:"video/0",type:"video"}]`},
		{name: "unsupported type", projection: `[{key:"video",type:"text"}]`},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			source := strings.Replace(mockPlugin, `export function listArtifacts() { return []; }
export function buildContentRequest() { throw new Error("artifact_not_found"); }`, `export function listArtifacts() { return `+testCase.projection+`; }
export function buildContentRequest(ctx) { return {url:ctx.baseUrl+"/content",method:"GET"}; }
`, 1)
			plugin, err := pluginruntime.NewRegistry().Register(source, pluginruntime.Options{})
			require.NoError(t, err)
			adaptor := New(plugin)
			_, err = adaptor.ListArtifacts(&model.Task{TaskID: "task", Status: model.TaskStatusSuccess, Data: []byte(`{}`)})
			require.Error(t, err)
		})
	}
}

func TestTaskAdaptorAllowsExplicitCredentiallessCDNRequest(t *testing.T) {
	source := strings.Replace(mockPlugin, `export function listArtifacts() { return []; }
export function buildContentRequest() { throw new Error("artifact_not_found"); }`, `export function listArtifacts() { return [{key:"video",type:"video"}]; }
export function buildContentRequest(ctx) { return {url:"https://cdn.example/video.mp4",method:ctx.clientRequest.method,credentialless:true}; }
`, 1)
	plugin, err := pluginruntime.NewRegistry().Register(source, pluginruntime.Options{})
	require.NoError(t, err)
	adaptor := New(plugin)
	adaptor.Init(&relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: "https://provider.example"}})
	descriptor, err := adaptor.BuildContentRequest(
		&model.Task{TaskID: "task", Data: []byte(`{}`)},
		"video",
		channel.TaskArtifactClientRequest{Method: http.MethodGet},
	)
	require.NoError(t, err)
	require.NotNil(t, descriptor)
	assert.True(t, descriptor.Credentialless)
	assert.Equal(t, "https://cdn.example/video.mp4", descriptor.URL)
}

func TestTaskAdaptorAllowsInlineDataArtifactRequest(t *testing.T) {
	source := strings.Replace(mockPlugin, `export function listArtifacts() { return []; }
export function buildContentRequest() { throw new Error("artifact_not_found"); }`, `export function listArtifacts() { return [{key:"video",type:"video"}]; }
export function buildContentRequest(ctx) { return {url:"data:video/mp4;base64,aGVsbG8=",method:ctx.clientRequest.method}; }
`, 1)
	plugin, err := pluginruntime.NewRegistry().Register(source, pluginruntime.Options{})
	require.NoError(t, err)
	adaptor := New(plugin)
	adaptor.Init(&relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: "https://provider.example"}})

	descriptor, err := adaptor.BuildContentRequest(
		&model.Task{TaskID: "task", Status: model.TaskStatusSuccess, Data: []byte(`{}`)},
		"video",
		channel.TaskArtifactClientRequest{Method: http.MethodGet},
	)
	require.NoError(t, err)
	require.NotNil(t, descriptor)
	assert.Equal(t, "data:video/mp4;base64,aGVsbG8=", descriptor.URL)
	assert.Equal(t, http.MethodGet, descriptor.Method)
}

func TestTaskAdaptorRejectsMalformedInlineDataArtifactURL(t *testing.T) {
	source := strings.Replace(mockPlugin, `export function listArtifacts() { return []; }
export function buildContentRequest() { throw new Error("artifact_not_found"); }`, `export function listArtifacts() { return [{key:"video",type:"video"}]; }
export function buildContentRequest(ctx) { return {url:"data:video/mp4;base64foo,aGVsbG8=",method:ctx.clientRequest.method}; }
`, 1)
	plugin, err := pluginruntime.NewRegistry().Register(source, pluginruntime.Options{})
	require.NoError(t, err)
	adaptor := New(plugin)
	adaptor.Init(&relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: "https://provider.example"}})

	_, err = adaptor.BuildContentRequest(
		&model.Task{TaskID: "task", Status: model.TaskStatusSuccess, Data: []byte(`{}`)},
		"video",
		channel.TaskArtifactClientRequest{Method: http.MethodGet},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be base64")
}

func TestTaskAdaptorMapsJSContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/submit":
			assert.Equal(t, "submit", r.Header.Get("X-Plugin"))
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			assert.JSONEq(t, `{"prompt":"hello"}`, string(body))
			_, _ = w.Write([]byte(`{"id":"upstream-1"}`))
		case "/tasks/upstream-1":
			assert.Equal(t, "query", r.Header.Get("X-Plugin"))
			_, _ = w.Write([]byte(`{"id":"upstream-1","url":"https://cdn.example/video.mp4"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	registry := pluginruntime.NewRegistry()
	plugin, err := registry.Register(mockPlugin, pluginruntime.Options{Key: "mock-task", Version: "1.0.0"})
	require.NoError(t, err)
	adaptor := New(plugin)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: server.URL, ApiKey: "secret"}, OriginModelName: "client-model", TaskRelayInfo: &relaycommon.TaskRelayInfo{PublicTaskID: "task_public"}}
	adaptor.Init(info)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	c.Set("task_request", relaycommon.TaskSubmitReq{Prompt: "hello"})
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	assert.Equal(t, "text_to_video", info.Action)
	assert.Equal(t, "mock-v1", info.OriginModelName)
	assert.Equal(t, "mock-upstream", info.UpstreamModelName)
	assert.Equal(t, []string{"mock-v1"}, adaptor.GetModelList())
	assert.Equal(t, "Mock Task", adaptor.GetChannelName())
	assert.Equal(t, map[string]float64{"seconds": 5}, adaptor.EstimateBilling(c, info))

	requestBody, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	url, err := adaptor.BuildRequestURL(info)
	require.NoError(t, err)
	assert.Equal(t, server.URL+"/submit", url)
	req := httptest.NewRequest(http.MethodPost, url, nil)
	require.NoError(t, adaptor.BuildRequestHeader(c, req, info))
	assert.Equal(t, "submit", req.Header.Get("X-Plugin"))

	resp, err := adaptor.DoRequest(c, info, requestBody)
	require.NoError(t, err)
	parsed, taskErr := adaptor.ParseResponse(c, resp, info)
	require.Nil(t, taskErr)
	require.NotNil(t, parsed)
	assert.Equal(t, "upstream-1", parsed.UpstreamTaskID)
	assert.JSONEq(t, `{"accepted":true,"status":200}`, string(parsed.TaskData))
	assert.Nil(t, parsed.ClientResponse)
	assert.Empty(t, recorder.Body.String(), "response parsing must not write before the durable task barrier")
	assert.Equal(t, map[string]float64{"seconds": 7}, adaptor.AdjustBillingOnSubmit(info, []byte(`{"seconds":7}`)))

	queryResp, err := adaptor.FetchTask(server.URL, "secret", &model.Task{
		Action:      info.Action,
		PrivateData: model.TaskPrivateData{UpstreamTaskID: parsed.UpstreamTaskID},
	}, "")
	require.NoError(t, err)
	queryBody, err := io.ReadAll(queryResp.Body)
	require.NoError(t, err)
	require.NoError(t, queryResp.Body.Close())
	result, err := adaptor.ParseTaskResult(&model.Task{}, queryResp, queryBody)
	require.NoError(t, err)
	assert.Equal(t, "SUCCESS", result.Status)
	assert.Equal(t, "https://cdn.example/video.mp4", result.Url)
	assert.Zero(t, adaptor.AdjustBillingOnComplete(&model.Task{}, result))
	assert.Equal(t, 23, result.TotalTokens)

	rendered, err := adaptor.ConvertToOpenAIVideo(&model.Task{TaskID: "task_public", Status: model.TaskStatusSuccess})
	require.NoError(t, err)
	assert.JSONEq(t, `{
		"id":"task_public",
		"object":"video",
		"model":"",
		"status":"completed",
		"progress":0,
		"created_at":0
	}`, string(rendered))
	_, err = plugin.Engine.Export(context.Background(), "meta")
	require.NoError(t, err)
}

func TestTaskAdaptorPreservesImmediateTaskResultFields(t *testing.T) {
	source := strings.Replace(mockPlugin,
		`export function parseSubmitResponse(ctx, resp) {
  return {
    taskId: resp.body.id,
    taskData: {accepted: true, status: resp.statusCode},
  };
}`,
		`export function parseSubmitResponse(ctx, resp) {
  return {
    taskId: resp.body.id,
    immediate: {
      taskId: resp.body.id,
      status: "SUCCESS",
      progress: "80%",
      remoteUrl: "https://cdn.example/immediate.mp4",
      completionTokens: 12,
      totalTokens: 34,
      state: {phase: "ready"},
      usageFacts: {upstreamUnits: 34},
    },
  };
}`,
		1,
	)
	plugin, err := pluginruntime.NewRegistry().Register(source, pluginruntime.Options{Key: "immediate-task", Version: "1.0.0"})
	require.NoError(t, err)
	adaptor := New(plugin)
	info := &relaycommon.RelayInfo{
		ChannelMeta:   &relaycommon.ChannelMeta{ChannelBaseUrl: "https://provider.example"},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{PublicTaskID: "task_public"},
	}
	adaptor.Init(info)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	response := &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(`{"id":"upstream-immediate"}`)),
	}

	parsed, taskErr := adaptor.ParseResponse(c, response, info)
	require.Nil(t, taskErr)
	require.NotNil(t, parsed)
	require.NotNil(t, parsed.Immediate)
	assert.Equal(t, "https://cdn.example/immediate.mp4", parsed.Immediate.RemoteUrl)
	assert.Equal(t, 12, parsed.Immediate.CompletionTokens)
	assert.Equal(t, 34, parsed.Immediate.TotalTokens)
	assert.JSONEq(t, `{"phase":"ready"}`, string(parsed.Immediate.PluginState))
	assert.Equal(t, float64(34), parsed.Immediate.UsageFacts["upstreamUnits"])
}

func TestTaskAdaptorSanitizesOpenAIVideoRendererOutput(t *testing.T) {
	source := `
export const meta = {
  apiVersion: 1, key: "safe-video", name: "Safe Video", version: "1.0.0",
  author: {name: "Test"}, models: ["model"], fetchMode: "per_task", protocols: ["openai_video"],
};
export function buildSubmitRequest(ctx) { return {url: ctx.baseUrl + "/submit"}; }
export function parseSubmitResponse() { return {taskId: "upstream"}; }
export function buildQueryRequest(ctx) { return {url: ctx.baseUrl + "/query"}; }
export function parseTaskResult() { return {status: "SUCCESS"}; }
export function listArtifacts() { return []; }
export function buildContentRequest() { throw new Error("artifact_not_found"); }
export const protocols = {openai_video: {
decodeRequest: function(ctx) { return {kind:"submit", model:ctx.model, requestBody:ctx.body.value}; },
render: function() {
  return {
    id: "upstream-id",
    task_id: "upstream-task-id",
    object: "provider-video",
    model: "model",
    status: "completed",
    progress: 100,
    created_at: 10,
    completed_at: 20,
    metadata: {
      url: "https://upstream.example/video.mp4",
      URL: "https://upstream.example/uppercase.mp4",
      label: "safe",
    },
    url: "https://upstream.example/top-level.mp4",
    upstream_url: "https://upstream.example/unknown.mp4",
    provider_payload: {task_id: "upstream-task-id"},
  };
}}};
`
	plugin, err := pluginruntime.NewRegistry().Register(source, pluginruntime.Options{})
	require.NoError(t, err)
	adaptor := New(plugin)

	rendered, err := adaptor.ConvertToOpenAIVideo(&model.Task{
		TaskID:     "task_public",
		Status:     model.TaskStatusInProgress,
		Properties: model.Properties{OriginModelName: "origin-model"},
	})
	require.NoError(t, err)

	var video dto.OpenAIVideo
	require.NoError(t, common.Unmarshal(rendered, &video))
	assert.Equal(t, "task_public", video.ID)
	assert.Equal(t, "video", video.Object)
	assert.Empty(t, video.TaskID)
	assert.Equal(t, "origin-model", video.Model)
	assert.Zero(t, video.CompletedAt)
	assert.Equal(t, map[string]any{"label": "safe"}, video.Metadata)

	var fields map[string]any
	require.NoError(t, common.Unmarshal(rendered, &fields))
	assert.NotContains(t, fields, "url")
	assert.NotContains(t, fields, "upstream_url")
	assert.NotContains(t, fields, "provider_payload")
	assert.NotContains(t, fields, "completed_at")
	assert.NotContains(t, string(rendered), "upstream.example")
	assert.NotContains(t, string(rendered), "upstream-task-id")
}

func TestTaskAdaptorDropsSignedLocatorMetadataValues(t *testing.T) {
	source := `
export const meta = {
  apiVersion: 1, key: "safe-video-signed", name: "Safe Video", version: "1.0.0",
  author: {name: "Test"}, models: ["model"], fetchMode: "per_task", protocols: ["openai_video"],
};
export function buildSubmitRequest(ctx) { return {url: ctx.baseUrl + "/submit"}; }
export function parseSubmitResponse() { return {taskId: "upstream"}; }
export function buildQueryRequest(ctx) { return {url: ctx.baseUrl + "/query"}; }
export function parseTaskResult() { return {status: "SUCCESS"}; }
export function listArtifacts() { return []; }
export function buildContentRequest() { throw new Error("artifact_not_found"); }
export const protocols = {openai_video: {
decodeRequest: function(ctx) { return {kind:"submit", model:ctx.model, requestBody:ctx.body.value}; },
render: function() {
  return {
    id: "upstream-id",
    object: "video",
    model: "model",
    status: "in_progress",
    metadata: {
      label: "safe",
      signed: "https://cdn.example/signed.mp4?token=secret",
      download_url: "https://cdn.example/download.mp4",
      poster: "https://cdn.example/poster.jpg"
    }
  };
}}};
`
	plugin, err := pluginruntime.NewRegistry().Register(source, pluginruntime.Options{})
	require.NoError(t, err)
	adaptor := New(plugin)

	rendered, err := adaptor.ConvertToOpenAIVideo(&model.Task{
		TaskID:     "task_public",
		Status:     model.TaskStatusInProgress,
		Properties: model.Properties{OriginModelName: "origin-model"},
	})
	require.NoError(t, err)

	var video dto.OpenAIVideo
	require.NoError(t, common.Unmarshal(rendered, &video))
	assert.Equal(t, map[string]any{"label": "safe"}, video.Metadata)
	assert.NotContains(t, string(rendered), "cdn.example")
	assert.NotContains(t, string(rendered), "token=secret")
}

func TestConvertToOpenAIVideoProjectsHostResultURL(t *testing.T) {
	source := `
export const meta = {
  apiVersion: 1, key: "safe-video-url", name: "Safe Video", version: "1.0.0",
  author: {name: "Test"}, models: ["model"], fetchMode: "per_task", protocols: ["openai_video"],
};
export function buildSubmitRequest(ctx) { return {url: ctx.baseUrl + "/submit"}; }
export function parseSubmitResponse() { return {taskId: "upstream"}; }
export function buildQueryRequest(ctx) { return {url: ctx.baseUrl + "/query"}; }
export function parseTaskResult() { return {status: "SUCCESS"}; }
export function listArtifacts() { return []; }
export function buildContentRequest() { throw new Error("artifact_not_found"); }
export const protocols = {openai_video: {
decodeRequest: function(ctx) { return {kind:"submit", model:ctx.model, requestBody:ctx.body.value}; },
render: function() {
  return {
    id: "upstream-id",
    object: "video",
    model: "model",
    status: "completed",
    metadata: {url: "https://upstream.example/video.mp4"},
    url: "https://upstream.example/top-level.mp4",
  };
}}};
`
	plugin, err := pluginruntime.NewRegistry().Register(source, pluginruntime.Options{})
	require.NoError(t, err)
	adaptor := New(plugin)

	task := &model.Task{
		TaskID: "task_public",
		Status: model.TaskStatusSuccess,
	}
	task.PrivateData.ResultURL = "https://api.example/v1/videos/task_public/content"

	rendered, err := adaptor.ConvertToOpenAIVideo(task)
	require.NoError(t, err)

	var video dto.OpenAIVideo
	require.NoError(t, common.Unmarshal(rendered, &video))
	assert.Equal(t, "https://api.example/v1/videos/task_public/content", video.Metadata["url"])
	assert.NotContains(t, string(rendered), "upstream.example")
}

func TestTaskAdaptorPreservesOpenAIVideoFailureSlotsAndOwnsLifecycle(t *testing.T) {
	source := strings.Replace(mockPlugin, `render: function(ctx, task) { return {id: task.task_id, status: "completed"}; }`, `render: function() { return {id:"provider", object:"provider", model:"provider-model", status:"completed", progress:100, created_at:99, completed_at:20, error:{code:"provider_error",message:"provider rejected request"}}; }`, 1)
	plugin, err := pluginruntime.NewRegistry().Register(source, pluginruntime.Options{})
	require.NoError(t, err)
	adaptor := New(plugin)
	task := &model.Task{
		TaskID:     "task_public",
		Status:     model.TaskStatusFailure,
		FailReason: "provider secret",
		CreatedAt:  10,
		UpdatedAt:  20,
		Properties: model.Properties{OriginModelName: "origin-model"},
	}

	rendered, err := adaptor.ConvertToOpenAIVideo(task)

	require.NoError(t, err)
	assert.JSONEq(t, `{"id":"task_public","object":"video","model":"origin-model","status":"failed","progress":0,"created_at":10,"error":{"message":"task failed","code":"task_failed"}}`, string(rendered))
}

func TestConvertToOpenAIVideoSanitizesPluginFailureAndDropsInternalFields(t *testing.T) {
	source := strings.Replace(mockPlugin, `render: function(ctx, task) { return {id: task.task_id, status: "completed"}; }`, `render: function() { return {id:"provider", object:"provider", model:"provider-model", status:"completed", progress:100, created_at:99, completed_at:20, error:{code:"provider_error",message:"pq: password authentication failed for user newapi"}, metadata:{url:"https://upstream.example/raw.mp4", signed:"leak"}, url:"https://upstream.example/top-level.mp4", upstream_debug:"dsn=postgres://user:secret@10.0.0.8:5432/newapi"}; }`, 1)
	plugin, err := pluginruntime.NewRegistry().Register(source, pluginruntime.Options{})
	require.NoError(t, err)
	adaptor := New(plugin)
	task := &model.Task{
		TaskID:     "task_public",
		Status:     model.TaskStatusFailure,
		FailReason: "pq: password authentication failed for user newapi",
		CreatedAt:  10,
		UpdatedAt:  20,
		Properties: model.Properties{OriginModelName: "origin-model"},
	}

	rendered, err := adaptor.ConvertToOpenAIVideo(task)

	require.NoError(t, err)
	var video dto.OpenAIVideo
	require.NoError(t, common.Unmarshal(rendered, &video))
	require.Equal(t, "task_public", video.ID)
	require.Equal(t, dto.VideoStatusFailed, video.Status)
	require.NotNil(t, video.Error)
	require.Equal(t, model.TaskPublicInternalFailReason, video.Error.Message)
	require.Equal(t, "task_failed", video.Error.Code)
	require.Nil(t, video.Metadata)
	require.NotContains(t, string(rendered), "password")
	require.NotContains(t, string(rendered), "upstream.example")
	require.NotContains(t, string(rendered), "upstream_debug")
	require.NotContains(t, string(rendered), "10.0.0.8")
}

func TestTaskAdaptorBoundsNativeUsageBeforeQuotaCalculation(t *testing.T) {
	source := `
export const meta = {
  apiVersion: 1, key: "bounded-usage", name: "Bounded Usage", version: "1.0.0",
  author: {name: "Test"},
  models: ["model"], fetchMode: "per_task",
  usageSchema: {
    duration: {type: "number", unit: "second"},
    count: {type: "number", unit: "count"},
    tokens: {type: "number", unit: "token"},
    mode: {enum: ["std", "pro"]},
  },
  usageExamples: [{label: "std · 1s", facts: {duration: 1, count: 1, tokens: 1, mode: "std"}}],
};
export function buildSubmitRequest(ctx) {
  return {url: ctx.baseUrl + "/submit", method: "POST", body: {}};
}
export function parseSubmitResponse() { return {taskId: "1"}; }
export function buildQueryRequest() { return {url: "https://example.com"}; }
export function parseTaskResult() { return {status: "SUCCESS"}; }
export function extractUsage(ctx) {
  const entries = (ctx.requestBody || {}).hookUsageEntries || [];
  const facts = {};
  entries.forEach(function(entry) { facts[entry.name] = entry.value; });
  return facts;
}
export function extractUsageOnSubmit(ctx, data) { return (data || {}).usage || {}; }
export function extractUsageOnComplete(task, result, body) { return (body || {}).completionUsage || {}; }
`
	plugin, err := pluginruntime.NewRegistry().Register(source, pluginruntime.Options{})
	require.NoError(t, err)

	newRequest := func(t *testing.T, requestBody map[string]any) (*TaskAdaptor, *gin.Context, *relaycommon.RelayInfo) {
		t.Helper()
		adaptor := New(plugin)
		info := &relaycommon.RelayInfo{
			ChannelMeta:   &relaycommon.ChannelMeta{ChannelBaseUrl: "https://provider.example"},
			TaskRelayInfo: &relaycommon.TaskRelayInfo{},
		}
		adaptor.Init(info)
		context, _ := gin.CreateTestContext(httptest.NewRecorder())
		context.Request = httptest.NewRequest(http.MethodPost, "/native/submit", nil)
		context.Set("task_request", requestBody)
		return adaptor, context, info
	}

	requestTests := []struct {
		name string
		body map[string]any
	}{
		{
			name: "duration in resolved metadata",
			body: map[string]any{"metadata": map[string]any{"duration": relaycommon.MaxTaskDurationSeconds + 1}},
		},
		{
			name: "count in resolved metadata",
			body: map[string]any{"metadata": map[string]any{"count": dto.MaxImageN + 1}},
		},
		{
			name: "declared enum in resolved metadata",
			body: map[string]any{"metadata": map[string]any{"mode": "turbo"}},
		},
		{
			name: "implicit duration key without declaration",
			body: map[string]any{"durationSeconds": relaycommon.MaxTaskDurationSeconds + 1},
		},
		{
			name: "implicit count key without declaration",
			body: map[string]any{"image_count": dto.MaxImageN + 1},
		},
		{
			name: "negative resolved duration",
			body: map[string]any{"duration": -1},
		},
		{
			name: "non-finite resolved duration",
			body: map[string]any{"duration": math.Inf(1)},
		},
		{
			name: "metadata cannot hide behind valid top-level duration",
			body: map[string]any{
				"duration": relaycommon.MaxTaskDurationSeconds,
				"metadata": map[string]any{"duration": relaycommon.MaxTaskDurationSeconds + 1},
			},
		},
		{
			name: "nested passthrough duration",
			body: map[string]any{
				"metadata": map[string]any{
					"parameters": map[string]any{"duration": relaycommon.MaxTaskDurationSeconds + 1},
				},
			},
		},
	}
	for _, testCase := range requestTests {
		t.Run(testCase.name, func(t *testing.T) {
			adaptor, context, info := newRequest(t, testCase.body)
			taskErr := adaptor.ValidateRequestAndSetAction(context, info)
			require.NotNil(t, taskErr)
			assert.Equal(t, "plugin_usage_invalid", taskErr.Code)
		})
	}

	hookTests := []struct {
		name  string
		usage map[string]any
	}{
		{
			name:  "duration returned only by extractUsage",
			usage: map[string]any{"duration": float64(relaycommon.MaxTaskDurationSeconds + 1)},
		},
		{
			name:  "count returned only by extractUsage",
			usage: map[string]any{"count": float64(dto.MaxImageN + 1)},
		},
		{
			name:  "enum returned only by extractUsage",
			usage: map[string]any{"mode": "turbo"},
		},
		{
			name:  "undeclared numeric ratio uses conservative host ceiling",
			usage: map[string]any{"custom_ratio": float64(relaycommon.MaxTaskDurationSeconds + 1)},
		},
		{
			name:  "negative hook ratio",
			usage: map[string]any{"custom_ratio": -1.0},
		},
		{
			name:  "non-finite hook ratio",
			usage: map[string]any{"custom_ratio": math.NaN()},
		},
	}
	for _, testCase := range hookTests {
		t.Run(testCase.name, func(t *testing.T) {
			entries := make([]any, 0, len(testCase.usage))
			for key, value := range testCase.usage {
				entries = append(entries, map[string]any{"name": key, "value": value})
			}
			adaptor, context, info := newRequest(t, map[string]any{"hookUsageEntries": entries})
			require.Nil(t, adaptor.ValidateRequestAndSetAction(context, info))
			ratios, err := adaptor.EstimateBillingValidated(context, info)
			require.Error(t, err)
			assert.Nil(t, ratios)
		})
	}

	t.Run("numeric strings remain valid in vendor request fields", func(t *testing.T) {
		adaptor, context, info := newRequest(t, map[string]any{
			"metadata": map[string]any{
				"duration": "5",
				"count":    "2",
				"mode":     "std",
			},
		})
		assert.Nil(t, adaptor.ValidateRequestAndSetAction(context, info))
	})

	t.Run("numeric strings from usage hooks are rejected", func(t *testing.T) {
		adaptor, context, info := newRequest(t, map[string]any{
			"hookUsageEntries": []any{map[string]any{"name": "duration", "value": "5"}},
		})
		require.Nil(t, adaptor.ValidateRequestAndSetAction(context, info))
		ratios, err := adaptor.EstimateBillingValidated(context, info)
		require.Error(t, err)
		assert.Nil(t, ratios)
	})

	t.Run("declared token facts use int32 saturation instead of duration cap", func(t *testing.T) {
		adaptor, context, info := newRequest(t, map[string]any{
			"hookUsageEntries": []any{
				map[string]any{"name": "tokens", "value": float64(500000)},
			},
		})
		require.Nil(t, adaptor.ValidateRequestAndSetAction(context, info))
		facts, err := adaptor.ExtractUsageFactsValidated(context, info)
		require.NoError(t, err)
		assert.EqualValues(t, 500000, facts["tokens"])

		ratios, err := adaptor.EstimateBillingValidated(context, info)
		require.NoError(t, err)
		assert.Equal(t, 500000.0, ratios["tokens"])
	})

	t.Run("declared token facts saturate at the int32 quota bound", func(t *testing.T) {
		adaptor, context, info := newRequest(t, map[string]any{
			"hookUsageEntries": []any{
				map[string]any{"name": "tokens", "value": float64(common.MaxQuota) + 1},
			},
		})
		require.Nil(t, adaptor.ValidateRequestAndSetAction(context, info))
		facts, err := adaptor.ExtractUsageFactsValidated(context, info)
		require.NoError(t, err)
		assert.EqualValues(t, common.MaxQuota, facts["tokens"])
	})

	t.Run("canonical maxima and enum are accepted", func(t *testing.T) {
		adaptor, context, info := newRequest(t, map[string]any{
			"duration": relaycommon.MaxTaskDurationSeconds,
			"count":    dto.MaxImageN,
			"mode":     "std",
			"hookUsageEntries": []any{
				map[string]any{"name": "duration", "value": float64(relaycommon.MaxTaskDurationSeconds)},
				map[string]any{"name": "count", "value": float64(dto.MaxImageN)},
				map[string]any{"name": "mode", "value": "pro"},
			},
		})
		require.Nil(t, adaptor.ValidateRequestAndSetAction(context, info))
		ratios, err := adaptor.EstimateBillingValidated(context, info)
		require.NoError(t, err)
		assert.Equal(t, map[string]float64{
			"duration": relaycommon.MaxTaskDurationSeconds,
			"count":    dto.MaxImageN,
		}, ratios)
	})

	t.Run("runtime error does not expose plugin-controlled usage key", func(t *testing.T) {
		adaptor, context, info := newRequest(t, map[string]any{
			"hookUsageEntries": []any{
				map[string]any{
					"name":  "https://private.invalid/?token=secret",
					"value": float64(relaycommon.MaxTaskDurationSeconds + 1),
				},
			},
		})
		require.Nil(t, adaptor.ValidateRequestAndSetAction(context, info))
		_, err := adaptor.EstimateBillingValidated(context, info)
		require.Error(t, err)
		assert.NotContains(t, err.Error(), "private.invalid")
		assert.NotContains(t, err.Error(), "secret")
	})

	for _, testCase := range []struct {
		name  string
		usage map[string]any
	}{
		{
			name:  "oversized completion duration is discarded",
			usage: map[string]any{"duration": relaycommon.MaxTaskDurationSeconds + 1},
		},
		{
			name:  "oversized completion count is discarded",
			usage: map[string]any{"count": dto.MaxImageN + 1},
		},
		{
			name:  "completion hook numeric string is discarded",
			usage: map[string]any{"duration": "5"},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			adaptor, _, _ := newRequest(t, map[string]any{})
			body, marshalErr := common.Marshal(map[string]any{"completionUsage": testCase.usage})
			require.NoError(t, marshalErr)
			result, parseErr := adaptor.ParseTaskResult(&model.Task{}, &http.Response{StatusCode: http.StatusOK, Header: make(http.Header)}, body)
			require.NoError(t, parseErr)
			assert.Nil(t, result.UsageFacts)
			assert.Zero(t, result.TotalTokens)
		})
	}

	t.Run("declared completion token unit is saturated instead of discarded", func(t *testing.T) {
		adaptor, _, _ := newRequest(t, map[string]any{})
		body, err := common.Marshal(map[string]any{"completionUsage": map[string]any{"tokens": 500000}})
		require.NoError(t, err)
		result, err := adaptor.ParseTaskResult(&model.Task{}, &http.Response{StatusCode: http.StatusOK, Header: make(http.Header)}, body)
		require.NoError(t, err)
		assert.EqualValues(t, 500000, result.UsageFacts["tokens"])
	})

	t.Run("declared credit facts keep sub-integer precision", func(t *testing.T) {
		source := `
export const meta = {
  apiVersion: 1, key: "credit-decimals", name: "Credit Decimals", version: "1.0.0",
  author: {name: "Test"}, models: ["model"], fetchMode: "per_task",
  usageSchema: {units: {type: "number", unit: "credit"}},
  usageExamples: [{label: "3.5 credits", facts: {units: 3.5}}],
};
export function buildSubmitRequest(ctx) { return {url: ctx.baseUrl + "/submit"}; }
export function parseSubmitResponse() { return {taskId: "task"}; }
export function buildQueryRequest() { return {url: "https://example.com"}; }
export function parseTaskResult() { return {status: "SUCCESS"}; }
export function extractUsage() { return {units: 3.5}; }
export function extractUsageOnComplete() { return {units: 3.5}; }
`
		plugin, err := pluginruntime.NewRegistry().Register(source, pluginruntime.Options{})
		require.NoError(t, err)
		adaptor := New(plugin)
		info := &relaycommon.RelayInfo{
			ChannelMeta:   &relaycommon.ChannelMeta{ChannelBaseUrl: "https://provider.example"},
			TaskRelayInfo: &relaycommon.TaskRelayInfo{},
		}
		adaptor.Init(info)
		context, _ := gin.CreateTestContext(httptest.NewRecorder())
		context.Request = httptest.NewRequest(http.MethodPost, "/native/submit", nil)
		context.Set("task_request", map[string]any{"model": "model"})
		require.Nil(t, adaptor.ValidateRequestAndSetAction(context, info))

		facts, err := adaptor.ExtractUsageFactsValidated(context, info)
		require.NoError(t, err)
		assert.Equal(t, 3.5, facts["units"])

		body, err := common.Marshal(map[string]any{})
		require.NoError(t, err)
		result, err := adaptor.ParseTaskResult(&model.Task{}, &http.Response{StatusCode: http.StatusOK, Header: make(http.Header)}, body)
		require.NoError(t, err)
		assert.Equal(t, 3.5, result.UsageFacts["units"])
	})

	t.Run("completion token facts are saturated instead of duration-capped", func(t *testing.T) {
		adaptor, _, _ := newRequest(t, map[string]any{})
		body, err := common.Marshal(map[string]any{"completionUsage": map[string]any{"upstreamUnits": 5000}})
		require.NoError(t, err)
		result, err := adaptor.ParseTaskResult(&model.Task{}, &http.Response{StatusCode: http.StatusOK, Header: make(http.Header)}, body)
		require.NoError(t, err)
		assert.Equal(t, 5000, result.TotalTokens)
		assert.EqualValues(t, 5000, result.UsageFacts["upstreamUnits"])
	})

	t.Run("invalid post-submit adjustment is discarded before recalculation", func(t *testing.T) {
		adaptor, _, info := newRequest(t, map[string]any{})
		ratios := adaptor.AdjustBillingOnSubmit(info, []byte(`{"usage":{"duration":1000000000000000}}`))
		assert.Nil(t, ratios)
	})
}

func TestTaskAdaptorSeparatesExpressionFactsFromLegacyBillingRatios(t *testing.T) {
	source := `
export const meta = {
  apiVersion: 1, key: "usage-purpose", name: "Usage Purpose", version: "1.0.0",
  author: {name: "Test"}, models: ["usage-model"], fetchMode: "per_task",
  usageSchema: {seconds: {type: "number", unit: "second"}},
};
export function buildSubmitRequest(ctx) { return {url: ctx.baseUrl + "/submit"}; }
export function parseSubmitResponse() { return {taskId: "task"}; }
export function buildQueryRequest(ctx) { return {url: ctx.baseUrl + "/query"}; }
export function parseTaskResult() { return {status: "SUCCESS"}; }
export function extractUsage(ctx) {
  return ctx.usagePurpose === "billing_ratios" ? {legacy_multiplier: 2} : {seconds: 5};
}
`
	plugin, err := pluginruntime.NewRegistry().Register(source, pluginruntime.Options{})
	require.NoError(t, err)
	adaptor := New(plugin)
	info := &relaycommon.RelayInfo{
		ChannelMeta:   &relaycommon.ChannelMeta{ChannelBaseUrl: "https://provider.example"},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
	}
	adaptor.Init(info)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodPost, "/submit", nil)
	context.Set("task_request", map[string]any{"model": "usage-model"})
	require.Nil(t, adaptor.ValidateRequestAndSetAction(context, info))

	facts, err := adaptor.ExtractUsageFactsValidated(context, info)
	require.NoError(t, err)
	assert.EqualValues(t, 5, facts["seconds"])
	assert.Len(t, facts, 1)

	ratios, err := adaptor.EstimateBillingValidated(context, info)
	require.NoError(t, err)
	assert.Equal(t, map[string]float64{"legacy_multiplier": 2}, ratios)
}

func TestTaskAdaptorAcceptsNormalizedLegacyTokenCounters(t *testing.T) {
	source := `
export const meta = {apiVersion:1,key:"normalized-tokens",name:"Normalized Tokens",version:"1.0.0",author:{name:"Test"},models:["m"],fetchMode:"per_task"};
export function buildSubmitRequest(ctx) { return {url: ctx.baseUrl + "/submit"}; }
export function parseSubmitResponse() { return {taskId: "task"}; }
export function buildQueryRequest(ctx) { return {url: ctx.baseUrl + "/query"}; }
export function parseTaskResult(ctx, body) { return {status: "SUCCESS", completionTokens: body.completion, totalTokens: body.total}; }
`
	plugin, err := pluginruntime.NewRegistry().Register(source, pluginruntime.Options{})
	require.NoError(t, err)
	adaptor := New(plugin)

	result, err := adaptor.ParseTaskResult(&model.Task{}, &http.Response{StatusCode: http.StatusOK, Header: make(http.Header)}, []byte(`{"completion":13,"total":17}`))
	require.NoError(t, err)
	assert.Equal(t, 13, result.CompletionTokens)
	assert.Equal(t, 17, result.TotalTokens)
	assert.Nil(t, result.UsageFacts)
}

func TestSubmitContextExposesOriginTasks(t *testing.T) {
	plugin, err := pluginruntime.NewRegistry().Register(mockPlugin, pluginruntime.Options{})
	require.NoError(t, err)
	adaptor := New(plugin)
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: "https://provider.example", ApiKey: "secret"},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{
			OriginTasks: []relaycommon.OriginTaskRef{{
				TaskID:         "task_pub_1",
				UpstreamTaskID: "cgt-upstream-1",
				Action:         "text_to_video",
				Status:         "SUCCESS",
				Data:           []byte(`{"id":"cgt-upstream-1"}`),
			}},
		},
	}
	adaptor.Init(info)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)

	ctx, err := adaptor.submitContext(c, info)
	require.NoError(t, err)

	originTasks, ok := ctx["originTasks"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, originTasks, 1)
	assert.Equal(t, "task_pub_1", originTasks[0]["taskId"])
	assert.Equal(t, "cgt-upstream-1", originTasks[0]["upstreamTaskId"])
	assert.Equal(t, "text_to_video", originTasks[0]["action"])
	assert.Equal(t, "SUCCESS", originTasks[0]["status"])
	assert.Equal(t, map[string]any{"id": "cgt-upstream-1"}, originTasks[0]["data"])
}

func TestSubmitContextOmitsOriginTasksWhenEmpty(t *testing.T) {
	plugin, err := pluginruntime.NewRegistry().Register(mockPlugin, pluginruntime.Options{})
	require.NoError(t, err)
	adaptor := New(plugin)
	info := &relaycommon.RelayInfo{
		ChannelMeta:   &relaycommon.ChannelMeta{ChannelBaseUrl: "https://provider.example", ApiKey: "secret"},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
	}
	adaptor.Init(info)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)

	ctx, err := adaptor.submitContext(c, info)
	require.NoError(t, err)

	_, ok := ctx["originTasks"]
	assert.False(t, ok)
}

func TestSubmitContextOriginTasksNilDataOnInvalidJSON(t *testing.T) {
	plugin, err := pluginruntime.NewRegistry().Register(mockPlugin, pluginruntime.Options{})
	require.NoError(t, err)
	adaptor := New(plugin)
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: "https://provider.example", ApiKey: "secret"},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{
			OriginTasks: []relaycommon.OriginTaskRef{{
				TaskID:         "task_pub_1",
				UpstreamTaskID: "cgt-upstream-1",
				Action:         "text_to_video",
				Status:         "SUCCESS",
				Data:           []byte("not-json"),
			}},
		},
	}
	adaptor.Init(info)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)

	ctx, err := adaptor.submitContext(c, info)
	require.NoError(t, err)

	originTasks, ok := ctx["originTasks"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, originTasks, 1)
	assert.Nil(t, originTasks[0]["data"])
}

func TestTaskAdaptorRejectsRequestHostOverride(t *testing.T) {
	source := strings.Replace(mockPlugin, `ctx.baseUrl + "/submit"`, `"https://attacker.example/steal"`, 1)
	plugin, err := pluginruntime.NewRegistry().Register(source, pluginruntime.Options{Key: "mock-task", Version: "1.0.0"})
	require.NoError(t, err)
	adaptor := New(plugin)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: "https://provider.example"}, TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	c.Set("task_request", relaycommon.TaskSubmitReq{Prompt: "hello"})
	taskErr := adaptor.ValidateRequestAndSetAction(c, info)
	require.NotNil(t, taskErr)
	assert.Contains(t, taskErr.Message, "not allowed")
}

const batchMockPlugin = `
export const meta = { apiVersion: 1, key: "mock-batch", name: "Mock Batch", version: "1.0.0", author: {name: "Test"}, channelTypes: [1002], models: ["batch-v1"], fetchMode: "batch" };
export function buildSubmitRequest(ctx) { return { url: ctx.baseUrl + "/submit", method: "POST", body: {} }; }
export function parseSubmitResponse(ctx, resp) { return { taskId: resp.body.id }; }
export function buildQueryRequest(ctx) { return { url: ctx.baseUrl + "/tasks/" + ctx.taskId }; }
export function parseTaskResult(ctx, body) { return { taskId: body.id, status: "SUCCESS" }; }
export function buildBatchQueryRequest(ctx, tasks) { return { url: ctx.baseUrl + "/batch", method: "POST", headers: { "X-Plugin": "batch" }, body: { ids: (tasks || []).map(function (task) { return task.taskId; }) } }; }
export function parseBatchResult(ctx, body) {
  return body.items.map(function (item) {
    return { taskId: item.id, action: item.action, status: item.status, progress: item.progress, url: (item.urls || [])[0] || "", finishTime: item.finish || 0, data: item };
  });
}
export function extractUsageOnComplete(task, result, body) { return {upstreamUnits: body.usage || 0}; }
`

// Covers the bridge half of the batch contract: FetchBatchTasks must build the
// upstream request from the plugin descriptor, and ParseBatchResult must key
// results by taskId, preserve the explicit result URL, and skip entries without
// a task id.
func TestTaskAdaptorBatchBridge(t *testing.T) {
	service.InitHttpClient()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/batch", r.URL.Path)
		require.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "batch", r.Header.Get("X-Plugin"))
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		assert.JSONEq(t, `{"ids":["task-a","task-b"]}`, string(body))
		_, _ = w.Write([]byte(`{"items":[
			{"id":"task-a","action":"music","status":"SUCCESS","progress":"100%","urls":["https://cdn.example/a1.mp3","https://cdn.example/a2.mp3"],"finish":1700000000,"usage":23},
			{"id":"task-b","status":"IN_PROGRESS","progress":"40%"},
			{"id":"","status":"SUCCESS"}
		]}`))
	}))
	defer server.Close()

	plugin, err := pluginruntime.NewRegistry().Register(batchMockPlugin, pluginruntime.Options{Key: "mock-batch", Version: "1.0.0"})
	require.NoError(t, err)
	adaptor := New(plugin)
	require.Equal(t, "batch", adaptor.FetchMode())

	tasks := []*model.Task{
		{PrivateData: model.TaskPrivateData{UpstreamTaskID: "task-a"}},
		{PrivateData: model.TaskPrivateData{UpstreamTaskID: "task-b"}},
	}
	resp, err := adaptor.FetchBatchTasks(server.URL, "secret", tasks, "")
	require.NoError(t, err)
	defer resp.Body.Close()
	payload, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	results, err := adaptor.ParseBatchResult(tasks, resp, payload)
	require.NoError(t, err)
	require.Len(t, results, 2, "entry without taskId must be skipped")

	done := results["task-a"]
	require.NotNil(t, done)
	assert.Equal(t, "music", done.Action)
	assert.Equal(t, "SUCCESS", done.TaskInfo.Status)
	assert.Equal(t, "100%", done.TaskInfo.Progress)
	assert.Equal(t, "https://cdn.example/a1.mp3", done.TaskInfo.Url)
	assert.Equal(t, int64(1700000000), done.FinishTime)
	assert.EqualValues(t, 23, done.TaskInfo.UsageFacts["upstreamUnits"])
	assert.Equal(t, 23, done.TaskInfo.TotalTokens)
	require.NotNil(t, done.Data)

	pending := results["task-b"]
	require.NotNil(t, pending)
	assert.Equal(t, "IN_PROGRESS", pending.TaskInfo.Status)
	assert.Equal(t, "40%", pending.TaskInfo.Progress)
	assert.Empty(t, pending.TaskInfo.Url)
}

const mappingOrderAdaptorPlugin = `
export const meta = {apiVersion:1,key:"map-order-adaptor",name:"Map Order Adaptor",version:"1.0.0",author:{name:"Test"},models:["declared-model"],fetchMode:"per_task"};
export function buildSubmitRequest(ctx) {
  return {url: ctx.baseUrl+"/submit", method:"POST", body:{upstreamModel: ctx.upstreamModel, model: ctx.model}};
}
export function parseSubmitResponse(){return {taskId:"1"};}
export function buildQueryRequest(){return {url:"https://provider.example"};}
export function parseTaskResult(){return {status:"SUCCESS"};}
`

func mappingOrderSubmitBody(t *testing.T, origin, mapping string) []byte {
	t.Helper()
	plugin, err := pluginruntime.NewRegistry().Register(mappingOrderAdaptorPlugin, pluginruntime.Options{})
	require.NoError(t, err)
	adaptor := New(plugin)
	info := &relaycommon.RelayInfo{
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelBaseUrl: "https://provider.example"},
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
		OriginModelName: origin,
	}
	adaptor.Init(info)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	if mapping != "" {
		c.Set("model_mapping", mapping)
	}
	c.Set("task_request", map[string]any{"prompt": "p"})
	info.UpstreamModelName = info.OriginModelName
	require.NoError(t, helper.ModelMappedHelper(c, info, nil))
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	body, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	raw, err := io.ReadAll(body)
	require.NoError(t, err)
	return raw
}

func TestTaskAdaptorBuildSubmitReceivesMappedUpstreamModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mapped := mappingOrderSubmitBody(t, "alias-model", `{"alias-model":"mid-model","mid-model":"declared-model"}`)
	var decoded map[string]any
	require.NoError(t, common.Unmarshal(mapped, &decoded))
	assert.Equal(t, "declared-model", decoded["upstreamModel"])
	assert.Equal(t, "alias-model", decoded["model"])

	withoutMapping := mappingOrderSubmitBody(t, "declared-model", "")
	emptyMapping := mappingOrderSubmitBody(t, "declared-model", "{}")
	assert.Equal(t, withoutMapping, emptyMapping)
	require.NoError(t, common.Unmarshal(withoutMapping, &decoded))
	assert.Equal(t, "declared-model", decoded["upstreamModel"])
	assert.Equal(t, "declared-model", decoded["model"])
}

// Polling has no relay info, so query hooks can only branch on the model when
// the host forwards the persisted task identities from the fetch body.
func TestTaskAdaptorFetchTaskExposesModelIdentities(t *testing.T) {
	service.InitHttpClient()
	var requested string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = r.URL.RequestURI()
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	source := `
export const meta = {apiVersion:1,key:"query-model",name:"Query Model",version:"1.0.0",author:{name:"Test"},models:["alias"],fetchMode:"per_task"};
export function buildSubmitRequest(ctx){return {url:ctx.baseUrl+"/submit"}}
export function parseSubmitResponse(){return {taskId:"1"}}
export function buildQueryRequest(ctx){return {url:ctx.baseUrl+"/tasks/"+ctx.model+"/"+ctx.upstreamModel+"/"+ctx.taskId,method:"GET"}}
export function parseTaskResult(){return {status:"SUCCESS"}}
`
	plugin, err := pluginruntime.NewRegistry().Register(source, pluginruntime.Options{})
	require.NoError(t, err)
	adaptor := New(plugin)

	testCases := []struct {
		name string
		task *model.Task
		want string
	}{
		{
			name: "mapped model",
			task: &model.Task{
				Properties:  model.Properties{OriginModelName: "alias", UpstreamModelName: "declared-model"},
				PrivateData: model.TaskPrivateData{UpstreamTaskID: "t1"},
			},
			want: "/tasks/alias/declared-model/t1",
		},
		{
			name: "unmapped model falls back to the origin name",
			task: &model.Task{
				Properties:  model.Properties{OriginModelName: "alias"},
				PrivateData: model.TaskPrivateData{UpstreamTaskID: "t1"},
			},
			want: "/tasks/alias/alias/t1",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			resp, fetchErr := adaptor.FetchTask(server.URL, "secret", testCase.task, "")
			require.NoError(t, fetchErr)
			require.NoError(t, resp.Body.Close())
			assert.Equal(t, testCase.want, requested)
		})
	}
}

func TestTaskAdaptorFetchTaskContextCancelsUpstreamRequest(t *testing.T) {
	service.InitHttpClient()
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()

	source := `
export const meta = {apiVersion:1,key:"query-cancel",name:"Query Cancel",version:"1.0.0",author:{name:"Test"},models:["m"],fetchMode:"per_task"};
export function buildSubmitRequest(){return {url:"https://provider.example/submit"}}
export function parseSubmitResponse(){return {taskId:"upstream-1"}}
export function buildQueryRequest(ctx){return {url:ctx.baseUrl+"/query"}}
export function parseTaskResult(){return {status:"IN_PROGRESS"}}
`
	plugin, err := pluginruntime.NewRegistry().Register(source, pluginruntime.Options{})
	require.NoError(t, err)
	adaptor := New(plugin)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		resp, fetchErr := adaptor.FetchTaskContext(ctx, server.URL, "secret", &model.Task{
			PrivateData: model.TaskPrivateData{UpstreamTaskID: "upstream-1"},
		}, "")
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		result <- fetchErr
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("upstream request was not started")
	}
	cancel()

	select {
	case err := <-result:
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("FetchTaskContext did not return after context cancellation")
	}
}

func TestTaskAdaptorQueryContextOmitsRequestBody(t *testing.T) {
	service.InitHttpClient()
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, common.DecodeJson(r.Body, &captured))
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	source := `
export const meta = {apiVersion:1,key:"query-ctx",name:"Query Ctx",version:"1.0.0",author:{name:"Test"},models:["alias"],fetchMode:"per_task"};
export function buildSubmitRequest(ctx){return {url:ctx.baseUrl+"/submit"}}
export function parseSubmitResponse(){return {taskId:"1",state:{req_key:"from-submit"}}}
export function buildQueryRequest(ctx){
  return {url:ctx.baseUrl+"/query",method:"POST",body:{
    keys: Object.keys(ctx).sort(),
    taskId: ctx.taskId,
    publicTaskId: ctx.publicTaskId,
    action: ctx.action,
    model: ctx.model,
    upstreamModel: ctx.upstreamModel,
    data: ctx.data,
    state: ctx.state,
    hasRequestBody: Object.prototype.hasOwnProperty.call(ctx, "requestBody")
  }};
}
export function parseTaskResult(ctx, body, response){
  return {status:"IN_PROGRESS",reason:String(response && response.status),url:ctx.taskId,state:{round:"poll"}};
}
`
	plugin, err := pluginruntime.NewRegistry().Register(source, pluginruntime.Options{})
	require.NoError(t, err)
	adaptor := New(plugin)
	adaptor.Init(&relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: server.URL, ApiKey: "secret"}})

	task := &model.Task{
		TaskID: "task_public",
		Action: constant.TaskActionImageToVideo,
		Properties: model.Properties{
			OriginModelName:   "alias",
			UpstreamModelName: "declared",
		},
		Data: []byte(`{"snapshot":true}`),
		PrivateData: model.TaskPrivateData{
			UpstreamTaskID: "upstream-1",
			PluginState:    []byte(`{"req_key":"kept"}`),
		},
	}
	resp, err := adaptor.FetchTask(server.URL, "secret", task, "")
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	assert.Equal(t, "upstream-1", captured["taskId"])
	assert.Equal(t, "task_public", captured["publicTaskId"])
	assert.Equal(t, constant.NormalizeTaskAction(constant.TaskActionImageToVideo), captured["action"])
	assert.Equal(t, "alias", captured["model"])
	assert.Equal(t, "declared", captured["upstreamModel"])
	assert.Equal(t, map[string]any{"snapshot": true}, captured["data"])
	assert.Equal(t, map[string]any{"req_key": "kept"}, captured["state"])
	assert.Equal(t, false, captured["hasRequestBody"])
	keys, ok := captured["keys"].([]any)
	require.True(t, ok)
	assert.NotContains(t, keys, "requestBody")

	result, err := adaptor.ParseTaskResult(task, &http.Response{StatusCode: http.StatusTeapot, Header: make(http.Header)}, []byte(`{"ok":true}`))
	require.NoError(t, err)
	assert.Equal(t, "IN_PROGRESS", result.Status)
	assert.Equal(t, "418", result.Reason)
	assert.Equal(t, "upstream-1", result.Url)
	assert.JSONEq(t, `{"round":"poll"}`, string(result.PluginState))
}

func TestTaskAdaptorParseSubmitResponsePersistsState(t *testing.T) {
	service.InitHttpClient()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"upstream-1"}`))
	}))
	defer server.Close()

	source := `
export const meta = {apiVersion:1,key:"submit-state",name:"Submit State",version:"1.0.0",author:{name:"Test"},models:["m"],fetchMode:"per_task"};
export function buildSubmitRequest(ctx){return {url:ctx.baseUrl+"/submit",method:"POST",body:{}}}
export function parseSubmitResponse(){return {taskId:"upstream-1",state:{req_key:"from-submit"}}}
export function buildQueryRequest(ctx){return {url:ctx.baseUrl+"/query"}}
export function parseTaskResult(){return {status:"SUCCESS"}}
`
	plugin, err := pluginruntime.NewRegistry().Register(source, pluginruntime.Options{})
	require.NoError(t, err)
	adaptor := New(plugin)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: server.URL, ApiKey: "secret"}, TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
	adaptor.Init(info)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	c.Set("task_request", relaycommon.TaskSubmitReq{Prompt: "hello"})
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	body, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	resp, err := adaptor.DoRequest(c, info, body)
	require.NoError(t, err)
	parsed, taskErr := adaptor.ParseResponse(c, resp, info)
	require.Nil(t, taskErr)
	require.NotNil(t, parsed)
	assert.JSONEq(t, `{"req_key":"from-submit"}`, string(parsed.PluginState))
}

func TestTaskAdaptorParseSubmitResponseRejectsOversizeBody(t *testing.T) {
	source := `
export const meta = {apiVersion:1,key:"submit-limit",name:"Submit Limit",version:"1.0.0",author:{name:"Test"},models:["m"],fetchMode:"per_task"};
export function buildSubmitRequest(){return {}}
export function parseSubmitResponse(){return {taskId:"x"}}
export function buildQueryRequest(){return {}}
export function parseTaskResult(){return {status:"SUCCESS"}}
`
	plugin, err := pluginruntime.NewRegistry().Register(source, pluginruntime.Options{})
	require.NoError(t, err)
	adaptor := New(plugin)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(strings.Repeat("a", int(service.MaxResponseBodyBytes)+1))),
	}

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	parsed, taskErr := adaptor.ParseResponse(c, resp, &relaycommon.RelayInfo{
		ChannelMeta:   &relaycommon.ChannelMeta{},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
	})
	require.Nil(t, parsed)
	require.NotNil(t, taskErr)
	assert.Equal(t, "read_response_body_failed", taskErr.Code)
	assert.Equal(t, http.StatusInternalServerError, taskErr.StatusCode)
}

func TestTaskAdaptorBatchQueryReceivesTaskObjects(t *testing.T) {
	service.InitHttpClient()
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, common.DecodeJson(r.Body, &captured))
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	defer server.Close()

	source := `
export const meta = {apiVersion:1,key:"batch-ctx",name:"Batch Ctx",version:"1.0.0",author:{name:"Test"},models:["m"],fetchMode:"batch"};
export function buildSubmitRequest(ctx){return {url:ctx.baseUrl+"/submit"}}
export function parseSubmitResponse(){return {taskId:"1"}}
export function buildQueryRequest(ctx){return {url:ctx.baseUrl+"/q"}}
export function parseTaskResult(){return {status:"SUCCESS"}}
export function buildBatchQueryRequest(ctx, tasks){
  return {url:ctx.baseUrl+"/batch",method:"POST",body:{
    ids: (tasks||[]).map(function(task){return task.taskId;}),
    models: (tasks||[]).map(function(task){return task.model;}),
    hasRequestBody: (tasks||[]).some(function(task){return Object.prototype.hasOwnProperty.call(task,"requestBody");})
  }};
}
export function parseBatchResult(){return [];}
`
	plugin, err := pluginruntime.NewRegistry().Register(source, pluginruntime.Options{})
	require.NoError(t, err)
	adaptor := New(plugin)
	tasks := []*model.Task{
		{Properties: model.Properties{OriginModelName: "model-a"}, PrivateData: model.TaskPrivateData{UpstreamTaskID: "task-a"}},
		{Properties: model.Properties{OriginModelName: "model-b"}, PrivateData: model.TaskPrivateData{UpstreamTaskID: "task-b"}},
	}
	resp, err := adaptor.FetchBatchTasks(server.URL, "secret", tasks, "")
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	assert.Equal(t, []any{"task-a", "task-b"}, captured["ids"])
	assert.Equal(t, []any{"model-a", "model-b"}, captured["models"])
	assert.Equal(t, false, captured["hasRequestBody"])
}

func allowPrivatePluginFetchRedirects(t *testing.T) {
	t.Helper()
	setting := system_setting.GetFetchSetting()
	oldAllowPrivate := setting.AllowPrivateIp
	oldPorts := append([]string(nil), setting.AllowedPorts...)
	setting.AllowPrivateIp = true
	setting.AllowedPorts = nil
	t.Cleanup(func() {
		setting.AllowPrivateIp = oldAllowPrivate
		setting.AllowedPorts = oldPorts
	})
}

func newQueryRedirectPlugin(t *testing.T) *TaskAdaptor {
	t.Helper()
	source := `
export const meta = {apiVersion:1,key:"redirect-query",name:"Redirect Query",version:"1.0.0",author:{name:"Test"},models:["m"],fetchMode:"per_task"};
export function buildSubmitRequest(){return {url:"https://example.com"}}
export function parseSubmitResponse(){return {taskId:"1"}}
export function buildQueryRequest(ctx){return {url:ctx.baseUrl+"/start",headers:{"X-API-Key":"secret-key"}}}
export function parseTaskResult(){return {status:"SUCCESS"}}
`
	plugin, err := pluginruntime.NewRegistry().Register(source, pluginruntime.Options{})
	require.NoError(t, err)
	return New(plugin)
}

func TestTaskAdaptorFetchTaskFollowsSameHostRedirect(t *testing.T) {
	service.InitHttpClient()
	allowPrivatePluginFetchRedirects(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/start":
			http.Redirect(w, r, "/done", http.StatusFound)
		case "/done":
			_, _ = w.Write([]byte(`{"status":"SUCCESS"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	adaptor := newQueryRedirectPlugin(t)
	resp, err := adaptor.FetchTask(server.URL, "secret", &model.Task{
		PrivateData: model.TaskPrivateData{UpstreamTaskID: "upstream-1"},
	}, "")
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.JSONEq(t, `{"status":"SUCCESS"}`, string(body))
}

func TestTaskAdaptorFetchTaskRejectsRedirectOutsideAllowedHosts(t *testing.T) {
	service.InitHttpClient()
	allowPrivatePluginFetchRedirects(t)

	sharedClient := service.GetHttpClient()
	require.NotNil(t, sharedClient)
	require.NotNil(t, sharedClient.CheckRedirect)
	originalRedirectPolicy := reflect.ValueOf(sharedClient.CheckRedirect).Pointer()

	var targetRequests atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetRequests.Add(1)
		w.WriteHeader(http.StatusTeapot)
	}))
	defer target.Close()

	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/stolen", http.StatusFound)
	}))
	defer source.Close()

	adaptor := newQueryRedirectPlugin(t)
	_, err := adaptor.FetchTask(source.URL, "secret", &model.Task{
		PrivateData: model.TaskPrivateData{UpstreamTaskID: "upstream-1"},
	}, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not allowed")
	assert.Zero(t, targetRequests.Load())
	assert.Equal(t, originalRedirectPolicy, reflect.ValueOf(sharedClient.CheckRedirect).Pointer(), "the cached client must not be mutated")
}

func TestStripPluginFetchRedirectCredentialsOnHTTPSToHTTPSameHost(t *testing.T) {
	original, err := http.NewRequest(http.MethodGet, "https://provider.example/start", nil)
	require.NoError(t, err)
	original.Header.Set("Authorization", "Bearer secret")
	original.Header.Set("X-API-Key", "secret-key")
	original.Header.Set("Cookie", "session=abc")

	redirect, err := http.NewRequest(http.MethodGet, "http://provider.example/done", nil)
	require.NoError(t, err)
	redirect.Header.Set("Authorization", "Bearer secret")
	redirect.Header.Set("X-API-Key", "secret-key")
	redirect.Header.Set("Cookie", "session=abc")

	stripPluginFetchRedirectCredentials(redirect, []*http.Request{original})

	assert.Empty(t, redirect.Header.Get("Authorization"))
	assert.Empty(t, redirect.Header.Get("X-API-Key"))
	assert.Empty(t, redirect.Header.Get("Cookie"))
}

func TestStripPluginFetchRedirectCredentialsKeepsSameOriginHTTPS(t *testing.T) {
	original, err := http.NewRequest(http.MethodGet, "https://provider.example/start", nil)
	require.NoError(t, err)
	original.Header.Set("Authorization", "Bearer secret")
	original.Header.Set("X-API-Key", "secret-key")

	redirect, err := http.NewRequest(http.MethodGet, "https://provider.example/done", nil)
	require.NoError(t, err)
	redirect.Header.Set("Authorization", "Bearer secret")
	redirect.Header.Set("X-API-Key", "secret-key")

	stripPluginFetchRedirectCredentials(redirect, []*http.Request{original})

	assert.Equal(t, "Bearer secret", redirect.Header.Get("Authorization"))
	assert.Equal(t, "secret-key", redirect.Header.Get("X-API-Key"))
}

func TestStripPluginFetchRedirectCredentialsStripsExplicitPortMismatch(t *testing.T) {
	original, err := http.NewRequest(http.MethodGet, "https://provider.example/start", nil)
	require.NoError(t, err)
	original.Header.Set("Authorization", "Bearer secret")

	redirect, err := http.NewRequest(http.MethodGet, "https://provider.example:8443/done", nil)
	require.NoError(t, err)
	redirect.Header.Set("Authorization", "Bearer secret")

	stripPluginFetchRedirectCredentials(redirect, []*http.Request{original})

	assert.Empty(t, redirect.Header.Get("Authorization"))
}

func TestTaskAdaptorFetchTaskAllowsRedirectToConfiguredHost(t *testing.T) {
	service.InitHttpClient()
	allowPrivatePluginFetchRedirects(t)

	var gotAPIKey string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAPIKey = r.Header.Get("X-API-Key")
		_, _ = w.Write([]byte(`{"status":"SUCCESS"}`))
	}))
	defer target.Close()

	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/ok", http.StatusFound)
	}))
	defer source.Close()

	targetURL, err := url.Parse(target.URL)
	require.NoError(t, err)
	adaptor := newQueryRedirectPlugin(t)
	adaptor.plugin.Meta.AllowedHosts = []string{targetURL.Host}

	resp, err := adaptor.FetchTask(source.URL, "secret", &model.Task{
		PrivateData: model.TaskPrivateData{UpstreamTaskID: "upstream-1"},
	}, "")
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.JSONEq(t, `{"status":"SUCCESS"}`, string(body))
	assert.Empty(t, gotAPIKey, "cross-host plugin fetch redirects must not forward credential headers")
}
