package gemini

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/internal/convdiag"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
)

// FunctionParameters returns the Gemini function declaration field that
// carries schema, and its value. A schema that fits Gemini's typed Schema is
// cleaned as before. Any other schema goes unchanged to parametersJsonSchema
// when jsonSchema is true; otherwise it is cleaned and misfits names what the
// typed Schema cannot carry.
func FunctionParameters(schema any, jsonSchema bool) (field string, value any, misfits []string) {
	misfits = schemaMisfits(schema)
	object, _ := schema.(map[string]any)
	properties, hasProperties := object["properties"].(map[string]any)
	emptyObject := hasProperties && len(properties) == 0
	if emptyObject {
		for key, item := range object {
			switch key {
			case "type", "properties":
				continue
			case "required":
				if required, _ := item.([]any); len(required) == 0 {
					continue
				}
			}
			if _, allowed := geminiOpenAPISchemaAllowedFields[key]; allowed {
				misfits = append(misfits, "constraints on an empty object")
				break
			}
		}
	}
	if len(misfits) > 0 && jsonSchema {
		return "parametersJsonSchema", schema, nil
	}
	if emptyObject {
		return "parameters", nil, misfits
	}
	return "parameters", CleanFunctionParameters(schema), misfits
}

// ApplyResponseSchema sets the structured output schema from an OpenAI
// json_schema object. A schema that fits Gemini's typed Schema keeps the
// responseSchema cleanup. Any other schema is sent unchanged as
// responseJsonSchema when jsonSchema is true.
func ApplyResponseSchema(ctx context.Context, config *dto.GeminiChatGenerationConfig, rawFormat json.RawMessage, path string, jsonSchema bool) {
	if config == nil {
		return
	}
	var format dto.FormatJsonSchema
	if err := kitutil.Unmarshal(rawFormat, &format); err != nil {
		return
	}
	misfits := schemaMisfits(format.Schema)
	if len(misfits) > 0 && jsonSchema {
		var raw struct {
			Schema json.RawMessage `json:"schema"`
		}
		if err := kitutil.Unmarshal(rawFormat, &raw); err == nil && len(raw.Schema) > 0 {
			config.ResponseJsonSchema = raw.Schema
			return
		}
	}
	config.ResponseSchema = RemoveAdditionalProperties(format.Schema, 0)
	if len(misfits) > 0 {
		convdiag.Add(ctx, types.ConversionDiagnostic{
			Code:     "gemini_schema_keyword_dropped",
			Path:     path,
			Message:  fmt.Sprintf("Gemini response schema cannot carry %s", strings.Join(misfits, ", ")),
			Severity: types.ConversionDiagnosticWarning,
		})
	}
}

func schemaMisfits(schema any) []string {
	found := make(map[string]struct{})
	collectSchemaMisfits(schema, 0, found)
	misfits := make([]string, 0, len(found))
	for misfit := range found {
		misfits = append(misfits, misfit)
	}
	slices.Sort(misfits)
	return misfits
}

func collectSchemaMisfits(schema any, depth int, found map[string]struct{}) {
	if depth > geminiFunctionSchemaMaxDepth {
		found[fmt.Sprintf("nesting deeper than %d levels", geminiFunctionSchemaMaxDepth)] = struct{}{}
		return
	}
	value, ok := schema.(map[string]any)
	if !ok {
		if _, isBool := schema.(bool); isBool {
			found["boolean schemas"] = struct{}{}
		}
		return
	}
	for key, item := range value {
		if _, allowed := geminiOpenAPISchemaAllowedFields[key]; !allowed {
			if key != "additionalProperties" && key != "$schema" {
				found[key] = struct{}{}
			}
			continue
		}
		switch key {
		case "type":
			if _, isArray := item.([]any); isArray {
				found["type arrays"] = struct{}{}
			}
		case "enum":
			values, _ := item.([]any)
			if slices.ContainsFunc(values, func(value any) bool {
				_, isString := value.(string)
				return !isString
			}) {
				found["non-string enum values"] = struct{}{}
			}
		case "properties":
			properties, _ := item.(map[string]any)
			for _, property := range properties {
				collectSchemaMisfits(property, depth+1, found)
			}
		case "items":
			entries, isTuple := item.([]any)
			if !isTuple {
				collectSchemaMisfits(item, depth+1, found)
				continue
			}
			found["tuple items"] = struct{}{}
			for _, entry := range entries {
				collectSchemaMisfits(entry, depth+1, found)
			}
		case "anyOf":
			variants, _ := item.([]any)
			for _, variant := range variants {
				collectSchemaMisfits(variant, depth+1, found)
			}
		}
	}
}

var geminiSchemaInt64Fields = map[string]struct{}{
	"maxItems":      {},
	"minItems":      {},
	"maxLength":     {},
	"minLength":     {},
	"maxProperties": {},
	"minProperties": {},
}

// OpenAPISchemaToJSONSchema rewrites a schema in Gemini's OpenAPI subset as
// JSON Schema. Gemini-only keywords that JSON Schema cannot express are
// returned and dropped.
func OpenAPISchemaToJSONSchema(schema any) (any, []string) {
	dropped := make(map[string]struct{})
	converted := openAPISchemaToJSONSchema(schema, 0, dropped)
	keywords := make([]string, 0, len(dropped))
	for keyword := range dropped {
		keywords = append(keywords, keyword)
	}
	slices.Sort(keywords)
	return converted, keywords
}

func openAPISchemaToJSONSchema(schema any, depth int, dropped map[string]struct{}) any {
	value, ok := schema.(map[string]any)
	if !ok || depth >= geminiFunctionSchemaMaxDepth {
		return schema
	}
	converted := make(map[string]any, len(value))
	nullable := false
	for key, item := range value {
		switch key {
		case "type":
			typeName, isString := item.(string)
			if !isString {
				converted[key] = item
				continue
			}
			typeName = strings.ToLower(strings.TrimSpace(typeName))
			if typeName != "" && typeName != "type_unspecified" {
				converted[key] = typeName
			}
		case "nullable":
			nullable, _ = item.(bool)
		case "propertyOrdering":
			dropped[key] = struct{}{}
		case "example":
			if _, exists := value["examples"]; exists {
				dropped[key] = struct{}{}
				continue
			}
			converted["examples"] = []any{item}
		case "properties":
			properties, isMap := item.(map[string]any)
			if !isMap {
				converted[key] = item
				continue
			}
			convertedProperties := make(map[string]any, len(properties))
			for name, property := range properties {
				convertedProperties[name] = openAPISchemaToJSONSchema(property, depth+1, dropped)
			}
			converted[key] = convertedProperties
		case "items":
			converted[key] = openAPISchemaToJSONSchema(item, depth+1, dropped)
		case "anyOf":
			variants, isArray := item.([]any)
			if !isArray {
				converted[key] = item
				continue
			}
			convertedVariants := make([]any, len(variants))
			for index, variant := range variants {
				convertedVariants[index] = openAPISchemaToJSONSchema(variant, depth+1, dropped)
			}
			converted[key] = convertedVariants
		default:
			if _, isInt64 := geminiSchemaInt64Fields[key]; isInt64 {
				if text, isString := item.(string); isString {
					if number, err := strconv.ParseInt(strings.TrimSpace(text), 10, 64); err == nil {
						converted[key] = number
						continue
					}
				}
			}
			converted[key] = item
		}
	}
	if !nullable {
		return converted
	}
	switch typed := converted["type"].(type) {
	case string:
		converted["type"] = []any{typed, "null"}
	default:
		if variants, isArray := converted["anyOf"].([]any); isArray {
			converted["anyOf"] = append(variants, map[string]any{"type": "null"})
		}
	}
	if values, isArray := converted["enum"].([]any); isArray && !slices.Contains(values, nil) {
		converted["enum"] = append(values, nil)
	}
	return converted
}
