package imagemaster

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/imagepolicy"
)

func cloneObject(v map[string]any) map[string]any {
	out := make(map[string]any, len(v))
	for k, value := range v {
		out[k] = value
	}
	return out
}

func responseSnapshot(raw []byte, p *Plan, id string) (map[string]any, int, error) {
	if isDirectRequiredError(raw) {
		return nil, 0, fail(502, imagepolicy.DirectRequiredCode)
	}
	var result map[string]any
	if json.Unmarshal(raw, &result) != nil || result == nil {
		return nil, 0, fail(502, "invalid_gateway_response")
	}
	if p.Route == "images-edits" || p.Route == "images-generations" {
		data, _ := result["data"].([]any)
		if len(data) != 1 {
			return nil, 0, fail(502, "image_missing")
		}
		img, _ := data[0].(map[string]any)
		b64, _ := img["b64_json"].(string)
		if b64 == "" {
			return nil, 0, fail(502, "image_missing")
		}
		item := map[string]any{"id": "ig_" + id, "type": "image_generation_call", "status": "completed", "result": b64}
		if text, ok := img["revised_prompt"].(string); ok {
			item["revised_prompt"] = text
		}
		if f, ok := result["output_format"].(string); ok {
			item["output_format"] = f
		} else if f := p.options["output_format"]; f != "" {
			item["output_format"] = f
		}
		result = map[string]any{"id": "resp_" + id, "object": "response", "status": "completed", "model": p.RequestedModel,
			"created_at": time.Now().Unix(), "completed_at": time.Now().Unix(),
			"output": []any{item}, "usage": result["usage"], "error": nil}
	}
	responseID, _ := result["id"].(string)
	status, _ := result["status"].(string)
	output, ok := result["output"].([]any)
	if responseID == "" || !ok || (status != "completed" && status != "failed" && status != "incomplete") {
		return nil, 0, fail(502, "invalid_gateway_response")
	}
	count := 0
	for _, v := range output {
		item, ok := v.(map[string]any)
		if !ok {
			return nil, 0, fail(502, "invalid_gateway_response")
		}
		itemID, _ := item["id"].(string)
		kind, _ := item["type"].(string)
		if itemID == "" || kind == "" {
			return nil, 0, fail(502, "invalid_gateway_response")
		}
		if kind == "image_generation_call" && item["status"] == "completed" {
			if image, ok := item["result"].(string); ok && image != "" {
				count++
			}
		}
	}
	if status == "completed" && (count == 0 || result["error"] != nil) {
		return nil, 0, fail(502, "image_missing")
	}
	return result, count, nil
}

func isDirectRequiredError(raw []byte) bool {
	var v struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	return json.Unmarshal(raw, &v) == nil && v.Error.Code == imagepolicy.DirectRequiredCode
}

type eventWriter struct {
	w   http.ResponseWriter
	seq int
}

func (e *eventWriter) emit(kind string, fields map[string]any) error {
	fields["type"], fields["sequence_number"] = kind, e.seq
	e.seq++
	b, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	if _, err = fmt.Fprintf(e.w, "event: %s\ndata: %s\n\n", kind, b); err != nil {
		return err
	}
	if f, ok := e.w.(http.Flusher); ok {
		f.Flush()
	}
	return nil
}

func (e *eventWriter) replay(snapshot map[string]any) error {
	status, ok := snapshot["status"].(string)
	if !ok || (status != "completed" && status != "failed" && status != "incomplete") {
		return fail(502, "invalid_gateway_response")
	}
	output, ok := snapshot["output"].([]any)
	if !ok {
		return fail(502, "invalid_gateway_response")
	}
	start := cloneObject(snapshot)
	start["status"], start["output"], start["error"], start["usage"] = "in_progress", []any{}, nil, nil
	start["incomplete_details"] = nil
	delete(start, "completed_at")
	if err := e.emit("response.created", map[string]any{"response": start}); err != nil {
		return err
	}
	if err := e.emit("response.in_progress", map[string]any{"response": start}); err != nil {
		return err
	}
	for i, value := range output {
		item, ok := value.(map[string]any)
		if !ok {
			return fail(502, "invalid_gateway_response")
		}
		added := cloneObject(item)
		added["status"] = "in_progress"
		switch item["type"] {
		case "image_generation_call":
			added["result"] = nil
		case "message":
			added["content"] = []any{}
		case "reasoning":
			added["summary"] = []any{}
		}
		if err := e.emit("response.output_item.added", map[string]any{"output_index": i, "item": added}); err != nil {
			return err
		}
		switch item["type"] {
		case "message":
			parts, _ := item["content"].([]any)
			for j, value := range parts {
				part, ok := value.(map[string]any)
				if !ok {
					continue
				}
				fields := map[string]any{"item_id": item["id"], "output_index": i, "content_index": j}
				empty := cloneObject(part)
				partType, _ := part["type"].(string)
				switch partType {
				case "output_text":
					empty["text"], empty["annotations"] = "", []any{}
				case "refusal":
					empty["refusal"] = ""
				}
				event := cloneObject(fields)
				event["part"] = empty
				if err := e.emit("response.content_part.added", event); err != nil {
					return err
				}
				if partType == "output_text" || partType == "refusal" {
					field := "text"
					if part["type"] == "refusal" {
						field = "refusal"
					}
					event = cloneObject(fields)
					event["delta"] = part[field]
					if err := e.emit("response."+partType+".delta", event); err != nil {
						return err
					}
					if annotations, ok := part["annotations"].([]any); ok {
						for k, a := range annotations {
							event = cloneObject(fields)
							event["annotation_index"], event["annotation"] = k, a
							if err := e.emit("response.output_text.annotation.added", event); err != nil {
								return err
							}
						}
					}
					event = cloneObject(fields)
					event[field] = part[field]
					if err := e.emit("response."+partType+".done", event); err != nil {
						return err
					}
				}
				event = cloneObject(fields)
				event["part"] = part
				if err := e.emit("response.content_part.done", event); err != nil {
					return err
				}
			}
		case "reasoning":
			parts, _ := item["summary"].([]any)
			for j, value := range parts {
				part, ok := value.(map[string]any)
				if !ok {
					continue
				}
				fields := map[string]any{"item_id": item["id"], "output_index": i, "summary_index": j}
				empty := cloneObject(part)
				empty["text"] = ""
				event := cloneObject(fields)
				event["part"] = empty
				if err := e.emit("response.reasoning_summary_part.added", event); err != nil {
					return err
				}
				event = cloneObject(fields)
				event["delta"] = part["text"]
				if err := e.emit("response.reasoning_summary_text.delta", event); err != nil {
					return err
				}
				event = cloneObject(fields)
				event["text"] = part["text"]
				if err := e.emit("response.reasoning_summary_text.done", event); err != nil {
					return err
				}
				event = cloneObject(fields)
				event["part"] = part
				if err := e.emit("response.reasoning_summary_part.done", event); err != nil {
					return err
				}
			}
		case "image_generation_call":
			if item["status"] == "completed" {
				if err := e.emit("response.image_generation_call.completed", map[string]any{"item_id": item["id"], "output_index": i}); err != nil {
					return err
				}
			}
		}
		if err := e.emit("response.output_item.done", map[string]any{"output_index": i, "item": item}); err != nil {
			return err
		}
	}
	return e.emit("response."+status, map[string]any{"response": snapshot})
}
