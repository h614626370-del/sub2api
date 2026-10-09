package imagemaster

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	_ "image/jpeg"
	"image/png"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/netip"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"time"

	_ "golang.org/x/image/webp"
	"golang.org/x/sync/errgroup"
)

const maxImageBytes = 50 << 20
const maxImagePixels = 40_000_000

type imageInput struct {
	data []byte
	mime string
	url  string
}

type Plan struct {
	Model          string
	RequestedModel string
	Stream         bool
	Route          string
	images         []imageInput
	mask           *imageInput
	options        map[string]string
	prompt         string
}

func HasImageTool(body []byte) bool {
	var v map[string]any
	if json.Unmarshal(body, &v) != nil {
		return false
	}
	tools, _ := v["tools"].([]any)
	for _, value := range tools {
		tool, _ := value.(map[string]any)
		if tool["type"] == "image_generation" {
			return true
		}
	}
	return false
}

func Prepare(raw []byte) (*Plan, error) {
	var body map[string]any
	if json.Unmarshal(raw, &body) != nil || body == nil {
		return nil, fail(400, "invalid_json")
	}
	for name, value := range body {
		switch name {
		case "model", "input", "instructions", "tools", "tool_choice", "stream", "background",
			"previous_response_id", "conversation", "prompt":
		case "store":
			if value != nil && value != false {
				return nil, fail(400, "response_storage_not_supported")
			}
		default:
			if value != nil {
				return nil, fail(400, "unsupported_response_option")
			}
		}
	}
	model, _ := body["model"].(string)
	if !modelName.MatchString(model) {
		return nil, fail(400, "invalid_model")
	}
	stream, ok := body["stream"].(bool)
	if body["stream"] != nil && !ok {
		return nil, fail(400, "invalid_stream")
	}
	if value := body["background"]; value != nil && value != false {
		return nil, fail(400, "background_not_supported")
	}
	tools, _ := body["tools"].([]any)
	if len(tools) != 1 || body["previous_response_id"] != nil || body["conversation"] != nil || body["prompt"] != nil {
		return nil, fail(400, "direct_image_unsupported")
	}
	tool, _ := tools[0].(map[string]any)
	p := &Plan{RequestedModel: model, Stream: stream, Route: "images-generations", options: map[string]string{}}
	p.Model, _ = tool["model"].(string)
	if tool["type"] != "image_generation" || !modelName.MatchString(p.Model) || !strings.HasPrefix(p.Model, "gpt-image-") {
		return nil, fail(400, "invalid_image_model")
	}
	if tool["n"] != nil && tool["n"] != float64(1) {
		return nil, fail(400, "single_output_required")
	}
	if value := tool["partial_images"]; value != nil {
		n, ok := value.(float64)
		if !ok || n < 0 || n > 3 || n != float64(int(n)) {
			return nil, fail(400, "invalid_image_option")
		}
	}
	for name := range tool {
		switch name {
		case "type", "model", "n", "action", "partial_images", "input_image_mask",
			"size", "quality", "background", "output_format", "output_compression", "moderation", "input_fidelity", "style":
		default:
			return nil, fail(400, "unsupported_image_option")
		}
	}
	if action := tool["action"]; action != nil && action != "auto" && action != "edit" && action != "generate" {
		return nil, fail(400, "direct_image_unsupported")
	}
	switch choice := body["tool_choice"].(type) {
	case nil:
	case string:
		if choice != "auto" && choice != "required" {
			return nil, fail(400, "direct_image_unsupported")
		}
	case map[string]any:
		if choice["type"] != "image_generation" || len(choice) != 1 {
			return nil, fail(400, "direct_image_unsupported")
		}
	default:
		return nil, fail(400, "direct_image_unsupported")
	}
	var texts []string
	if body["instructions"] != nil {
		text, ok := body["instructions"].(string)
		if !ok {
			return nil, fail(400, "invalid_instructions")
		}
		texts = append(texts, text)
	}
	// Only self-contained single-turn input can be converted without a model
	// resolving hidden conversation state or rewriting the user's intent.
	var promptText []string
	switch input := body["input"].(type) {
	case string:
		promptText = append(promptText, input)
	case []any:
		if len(input) != 1 {
			return nil, fail(400, "direct_image_unsupported")
		}
		item, _ := input[0].(map[string]any)
		if item["role"] != "user" || (item["type"] != nil && item["type"] != "message") {
			return nil, fail(400, "direct_image_unsupported")
		}
		for name := range item {
			switch name {
			case "role", "type", "content":
			default:
				return nil, fail(400, "direct_image_unsupported")
			}
		}
		if text, ok := item["content"].(string); ok {
			promptText = append(promptText, text)
			break
		}
		content, ok := item["content"].([]any)
		if !ok {
			return nil, fail(400, "direct_image_unsupported")
		}
		for _, v := range content {
			part, _ := v.(map[string]any)
			switch part["type"] {
			case "input_text":
				if len(part) != 2 {
					return nil, fail(400, "direct_image_unsupported")
				}
				text, ok := part["text"].(string)
				if !ok {
					return nil, fail(400, "invalid_prompt")
				}
				promptText = append(promptText, text)
			case "input_image":
				if len(p.images) >= 8 {
					return nil, fail(400, "too_many_source_images")
				}
				img, err := parseImage(part)
				if err != nil {
					return nil, err
				}
				p.images = append(p.images, img)
			default:
				return nil, fail(400, "direct_image_unsupported")
			}
		}
	default:
		return nil, fail(400, "direct_image_unsupported")
	}
	if strings.TrimSpace(strings.Join(promptText, "\n\n")) == "" {
		return nil, fail(400, "prompt_required")
	}
	p.prompt = strings.Join(append(texts, promptText...), "\n\n")
	if len(p.images) > 0 {
		if tool["action"] == "generate" {
			return nil, fail(400, "image_action_conflict")
		}
		p.Route = "images-edits"
	} else if tool["action"] == "edit" || tool["input_image_mask"] != nil || tool["input_fidelity"] != nil {
		return nil, fail(400, "source_image_required")
	}
	for _, name := range []string{"size", "quality", "background", "output_format", "output_compression", "moderation", "input_fidelity", "style"} {
		if value := tool[name]; value != nil {
			if name == "output_compression" {
				n, ok := value.(float64)
				if !ok || n < 0 || n > 100 || n != float64(int(n)) {
					return nil, fail(400, "invalid_image_option")
				}
				p.options[name] = strconv.Itoa(int(n))
			} else {
				s, ok := value.(string)
				if !ok || len(s) > 128 || strings.TrimSpace(s) == "" {
					return nil, fail(400, "invalid_image_option")
				}
				p.options[name] = s
			}
		}
	}
	if tool["input_image_mask"] != nil {
		value, ok := tool["input_image_mask"].(map[string]any)
		if !ok {
			return nil, fail(400, "invalid_mask")
		}
		img, err := parseImage(value)
		if err != nil {
			return nil, err
		}
		p.mask = &img
	}
	return p, nil
}

func parseImage(part map[string]any) (imageInput, error) {
	for name, value := range part {
		switch name {
		case "type", "image_url", "file_id":
		case "detail":
			if value != nil && value != "auto" {
				return imageInput{}, fail(400, "unsupported_image_detail")
			}
		default:
			return imageInput{}, fail(400, "unsupported_image_option")
		}
	}
	if part["file_id"] != nil {
		return imageInput{}, fail(400, "file_id_not_supported")
	}
	value, ok := part["image_url"].(string)
	if !ok {
		return imageInput{}, fail(400, "invalid_image")
	}
	if strings.HasPrefix(value, "data:") {
		header, encoded, found := strings.Cut(value, ",")
		if !found || !strings.HasSuffix(header, ";base64") {
			return imageInput{}, fail(400, "invalid_image")
		}
		mime := strings.TrimSuffix(strings.TrimPrefix(header, "data:"), ";base64")
		if len(encoded) > base64.StdEncoding.EncodedLen(maxImageBytes) {
			return imageInput{}, fail(413, "image_too_large")
		}
		data, err := base64.StdEncoding.Strict().DecodeString(encoded)
		if err != nil {
			return imageInput{}, fail(400, "invalid_image")
		}
		return inspectImage(data, mime)
	}
	if _, err := remoteURL(value); err != nil {
		return imageInput{}, err
	}
	return imageInput{url: value}, nil
}

func inspectImage(data []byte, mime string) (imageInput, error) {
	if len(data) == 0 || len(data) > maxImageBytes {
		return imageInput{}, fail(413, "image_too_large")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "png" && format != "jpeg" && format != "webp") {
		return imageInput{}, fail(400, "invalid_image")
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > maxImagePixels {
		return imageInput{}, fail(413, "image_too_large")
	}
	actual := "image/" + format
	if mime != "" && actual != mime {
		return imageInput{}, fail(400, "image_type_mismatch")
	}
	return imageInput{data: data, mime: actual}, nil
}

var blockedPrefixes = func() []netip.Prefix {
	var out []netip.Prefix
	for _, s := range []string{"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8",
		"169.254.0.0/16", "172.16.0.0/12", "192.0.0.0/24", "192.0.2.0/24", "192.168.0.0/16",
		"198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4",
		"::/96", "::1/128", "::ffff:0:0/96", "64:ff9b::/96", "fc00::/7", "fe80::/10", "fec0::/10",
		"ff00::/8", "2001::/23", "2001:db8::/32", "2002::/16"} {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}()

func publicIP(ip netip.Addr) bool {
	if !ip.IsGlobalUnicast() || ip.Is4In6() {
		return false
	}
	for _, prefix := range blockedPrefixes {
		if prefix.Contains(ip) {
			return false
		}
	}
	return true
}

func remoteURL(value string) (*url.URL, error) {
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Hostname() == "" || u.Fragment != "" {
		return nil, fail(400, "image_url_blocked")
	}
	return u, nil
}

func downloadImage(ctx context.Context, value string) (imageInput, error) {
	if _, err := remoteURL(value); err != nil {
		return imageInput{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	// Resolve and pin public IPs at the socket boundary, including every redirect.
	// Never use the process proxy or forward the user's API key to an image URL.
	transport := &http.Transport{DisableCompression: true, DisableKeepAlives: true,
		TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 15 * time.Second,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, fail(400, "image_url_blocked")
			}
			addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
			if err != nil || len(addresses) == 0 {
				return nil, fail(400, "image_download_failed")
			}
			for _, addr := range addresses {
				if !publicIP(addr) {
					return nil, fail(400, "image_url_blocked")
				}
			}
			return (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, net.JoinHostPort(addresses[0].String(), port))
		}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 3 {
			return fail(400, "image_redirect_limit")
		}
		_, err := remoteURL(req.URL.String())
		return err
	}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, value, nil)
	if err != nil {
		return imageInput{}, fail(400, "image_url_blocked")
	}
	req.Header.Set("Accept-Encoding", "identity")
	res, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return imageInput{}, ctx.Err()
		}
		return imageInput{}, fail(400, "image_download_failed")
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != 200 || (res.Header.Get("Content-Encoding") != "" && res.Header.Get("Content-Encoding") != "identity") {
		return imageInput{}, fail(400, "image_download_failed")
	}
	if res.ContentLength > maxImageBytes {
		return imageInput{}, fail(413, "image_too_large")
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, maxImageBytes+1))
	if err != nil {
		return imageInput{}, fail(400, "image_download_failed")
	}
	img, err := inspectImage(data, "")
	if err != nil {
		return imageInput{}, err
	}
	decoded, _, err := image.Decode(bytes.NewReader(img.data))
	if err != nil {
		return imageInput{}, fail(400, "image_conversion_failed")
	}
	// PNG provides lossless normalization without a Node/sharp or CGO runtime.
	out := &boundedBuffer{limit: maxImageBytes}
	if err = png.Encode(out, decoded); err != nil {
		return imageInput{}, fail(413, "image_too_large")
	}
	return imageInput{data: out.Bytes(), mime: "image/png"}, nil
}

type imageLoader func(context.Context, string) (imageInput, error)

func (p *Plan) encode(ctx context.Context, maxBytes int, load imageLoader) ([]byte, string, error) {
	if p.Route == "images-generations" {
		body := map[string]any{"model": p.Model, "prompt": p.prompt, "n": 1, "stream": false, "response_format": "b64_json"}
		for name, value := range p.options {
			if name == "output_compression" {
				body[name], _ = strconv.Atoi(value)
			} else {
				body[name] = value
			}
		}
		b, err := json.Marshal(body)
		if len(b) > maxBytes {
			return nil, "", fail(413, "request_too_large")
		}
		return b, "application/json", err
	}
	if load == nil {
		load = downloadImage
	}
	inputs := append([]imageInput(nil), p.images...)
	if p.mask != nil {
		inputs = append(inputs, *p.mask)
	}
	group, groupCtx := errgroup.WithContext(ctx)
	for i := range inputs {
		if inputs[i].url != "" {
			group.Go(func() error {
				img, err := load(groupCtx, inputs[i].url)
				if err == nil {
					inputs[i] = img
				}
				return err
			})
		}
	}
	if err := group.Wait(); err != nil {
		return nil, "", err
	}
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	var total int
	for _, img := range inputs {
		total += len(img.data)
	}
	if total+len(p.prompt) > maxBytes {
		return nil, "", fail(413, "request_too_large")
	}
	out := &boundedBuffer{limit: maxBytes}
	form := multipart.NewWriter(out)
	fields := map[string]string{"model": p.Model, "prompt": p.prompt, "n": "1", "response_format": "b64_json"}
	for k, v := range p.options {
		fields[k] = v
	}
	for k, v := range fields {
		if err := form.WriteField(k, v); err != nil {
			return nil, "", err
		}
	}
	for i, img := range inputs {
		name := "image"
		if i == len(p.images) {
			name = "mask"
		}
		header := textproto.MIMEHeader{}
		header.Set("Content-Disposition", `form-data; name="`+name+`"; filename="source-`+strconv.Itoa(i)+`.`+strings.TrimPrefix(img.mime, "image/")+`"`)
		header.Set("Content-Type", img.mime)
		part, err := form.CreatePart(header)
		if err != nil {
			return nil, "", err
		}
		if _, err = part.Write(img.data); err != nil {
			return nil, "", err
		}
	}
	if err := form.Close(); err != nil {
		return nil, "", err
	}
	return out.Bytes(), form.FormDataContentType(), nil
}

type boundedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.Len() {
		return 0, fail(413, "payload_too_large")
	}
	return b.Buffer.Write(p)
}
