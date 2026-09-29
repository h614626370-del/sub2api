package service

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Retry only turns that have no executable client calls. Neither rejected
// Office transports nor their synthetic outputs are exposed to the client.
func (s *OpenAIGatewayService) bpsCorrectTools(ctx context.Context, account *Account, r *bpsRequest, response map[string]any, proxyURL string, maxSize int, keepalive func() error) (map[string]any, error) {
	officeRetries, missingRetries := 0, 0
	totalUsage := bpsReadUsage(response)
	var priorOutput []any
	var working map[string]any
	if err := bpsDecode(r.Body, &working); err != nil {
		return nil, err
	}
	for {
		output, ok := response["output"].([]any)
		if !ok {
			return response, nil
		}
		var rejected []map[string]any
		clientCalls := 0
		for _, raw := range output {
			item := bpsItem(raw)
			if item["type"] != "function_call" && item["type"] != "custom_tool_call" {
				continue
			}
			if _, err := bpsConvertTool(item, r); err == nil {
				clientCalls++
			} else if item["type"] == "function_call" && (item["name"] == "run_officejs" || item["name"] == "functions.run_officejs") && strings.TrimSpace(stringValue(item["call_id"])) != "" {
				rejected = append(rejected, item)
				slog.WarnContext(ctx, "bps_tool_rejected", "name", stringValue(item["name"]), "reason", err.Error())
			} else {
				return nil, err
			}
		}
		if clientCalls > 0 && len(rejected) > 0 {
			return nil, &bpsError{502, "bps_invalid_tool_call", "BPS mixed rejected and client tool calls"}
		}
		if clientCalls > 0 || (len(rejected) == 0 && (!r.RequireTool || bpsHasRefusal(output))) {
			if officeRetries+missingRetries > 0 {
				response["output"] = append(priorOutput, output...)
				usage, _ := response["usage"].(map[string]any)
				if usage == nil {
					usage = map[string]any{}
					response["usage"] = usage
				}
				usage["input_tokens"] = totalUsage.InputTokens
				usage["output_tokens"] = totalUsage.OutputTokens
				details, _ := usage["input_tokens_details"].(map[string]any)
				if details == nil {
					details = map[string]any{}
					usage["input_tokens_details"] = details
				}
				details["cached_tokens"] = totalUsage.CacheReadInputTokens
			}
			return response, nil
		}
		if len(rejected) > 0 {
			if officeRetries >= 3 {
				return nil, &bpsError{502, "bps_invalid_tool_call", "BPS repeatedly returned invalid client tool calls"}
			}
			officeRetries++
		} else {
			if missingRetries >= 1 {
				return nil, &bpsError{502, "bps_tool_required", "BPS did not return the required client tool"}
			}
			missingRetries++
		}
		input, _ := working["input"].([]any)
		if len(rejected) == 0 {
			input = append(input, output...)
			priorOutput = append(priorOutput, output...)
		} else {
			for _, raw := range output {
				item := bpsItem(raw)
				if item["type"] == "function_call" || item["type"] == "custom_tool_call" {
					continue
				}
				input = append(input, item)
				priorOutput = append(priorOutput, item)
			}
		}
		if len(rejected) > 0 {
			seen := make(map[string]bool)
			for _, item := range rejected {
				id := stringValue(item["call_id"])
				if seen[id] {
					continue
				}
				seen[id] = true
				input = append(input, item)
				input = append(input, map[string]any{"type": "function_call_output", "call_id": id, "output": "That call was not forwarded. Nothing ran on the client machine. Use run_officejs with a JSON envelope naming one declared client tool."})
			}
		}
		input = append(input, bpsMessage("developer", "The previous response did not execute a client tool. Call one declared client tool via run_officejs with a valid JSON envelope, or answer directly if no tool is required."))
		working["input"] = input
		metadata, _ := working["metadata"].(map[string]any)
		metadata["agent_iteration"] = strconv.Itoa(r.AgentIteration + officeRetries + missingRetries)
		body := []byte(bpsJSON(working))
		pictureBody, _, err := s.prepareOpenAIBPSImages(ctx, account, body, map[string]bool{})
		if err != nil {
			return nil, err
		}
		req, err := buildOpenAIBPSRequest(ctx, account, pictureBody)
		if err != nil {
			return nil, err
		}
		upstream, err := s.httpUpstream.Do(req, proxyURL, account.ID, account.Concurrency)
		if err != nil {
			return nil, err
		}
		if upstream.StatusCode < http.StatusOK || upstream.StatusCode >= http.StatusMultipleChoices {
			_ = upstream.Body.Close()
			return nil, &bpsError{502, "bps_tool_retry_failed", "BPS tool correction request failed"}
		}
		var terminal map[string]any
		pending := map[string]bool{}
		idle := 5 * time.Minute
		if s.cfg != nil {
			idle = time.Duration(s.cfg.Gateway.StreamDataIntervalTimeout) * time.Second
		}
		watched := bpsWatchUpstream(upstream.Body, idle)
		upstream.Body = watched
		interval := time.Duration(0)
		if r.Stream {
			interval = 15 * time.Second
			if s.cfg != nil {
				interval = time.Duration(s.cfg.Gateway.StreamKeepaliveInterval) * time.Second
			}
		}
		err = readBPSEventsWithKeepalive(ctx, upstream, maxSize, interval, func(typ string, payload map[string]any) error {
			if typ == "response.output_item.done" {
				item := bpsItem(payload["item"])
				if item["type"] == "function_call" || item["type"] == "custom_tool_call" {
					pending[stringValue(item["call_id"])] = true
				}
			}
			if typ == "response.completed" || typ == "response.done" {
				terminal, _ = payload["response"].(map[string]any)
			} else if typ == "response.failed" || typ == "response.incomplete" || typ == "error" {
				return &bpsError{502, "bps_tool_retry_failed", "BPS tool correction did not complete"}
			}
			return nil
		}, keepalive)
		watched.stop()
		_ = upstream.Body.Close()
		if err != nil {
			return nil, err
		}
		if terminal == nil || terminal["status"] != "completed" {
			return nil, &bpsError{502, "bps_tool_retry_failed", "BPS tool correction ended without a completed response"}
		}
		nextOutput, _ := terminal["output"].([]any)
		for _, raw := range nextOutput {
			item := bpsItem(raw)
			delete(pending, stringValue(item["call_id"]))
		}
		if len(pending) != 0 {
			return nil, &bpsError{502, "bps_invalid_tool_call", "BPS correction omitted an original tool item"}
		}
		usage := bpsReadUsage(terminal)
		totalUsage.InputTokens += usage.InputTokens
		totalUsage.OutputTokens += usage.OutputTokens
		totalUsage.CacheReadInputTokens += usage.CacheReadInputTokens
		response = terminal
	}
}

func bpsHasRefusal(output []any) bool {
	for _, raw := range output {
		item := bpsItem(raw)
		if item["type"] != "message" {
			continue
		}
		content, _ := item["content"].([]any)
		for _, part := range content {
			if bpsItem(part)["type"] == "refusal" {
				return true
			}
		}
	}
	return false
}
