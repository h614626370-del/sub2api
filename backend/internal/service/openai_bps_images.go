package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime/multipart"
	"net/textproto"
	"strings"
	"sync"

	_ "golang.org/x/image/webp"
)

const (
	bpsMaxImageBytes        = 20 << 20
	bpsMaxRequestImageBytes = 32 << 20
	bpsMaxImagePixels       = 64 << 20
	bpsMaxUploadResponse    = 64 << 10
	bpsPictureCacheSize     = 256
)

type bpsPictureCache struct {
	mu           sync.Mutex
	uploads      [16]sync.Mutex
	files        map[string]string
	order        []string
	refused      map[string]map[string]bool
	refusedOrder []string
}

func (s *OpenAIGatewayService) bpsPictureStore() *bpsPictureCache {
	s.bpsPictureCacheOnce.Do(func() {
		s.bpsPictureCache = &bpsPictureCache{files: make(map[string]string), refused: make(map[string]map[string]bool)}
	})
	return s.bpsPictureCache
}

func bpsDecodeImageDataURL(raw string) (string, []byte, error) {
	header, payload, ok := strings.Cut(raw, ",")
	if !ok {
		return "", nil, bpsInvalid("图片必须是 base64 编码的 PNG、JPEG、GIF 或 WebP")
	}
	fields := strings.Split(strings.ToLower(header), ";")
	if len(fields) != 2 || fields[0] != "data:image/png" && fields[0] != "data:image/jpeg" &&
		fields[0] != "data:image/gif" && fields[0] != "data:image/webp" || fields[1] != "base64" {
		return "", nil, bpsInvalid("图片必须是 base64 编码的 PNG、JPEG、GIF 或 WebP")
	}
	mime := strings.TrimPrefix(fields[0], "data:")
	if len(payload) > 4*((bpsMaxImageBytes+2)/3) {
		return "", nil, bpsInvalid("单张图片不能超过 20 MiB")
	}
	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil || len(data) == 0 {
		return "", nil, bpsInvalid("图片的 base64 编码无效")
	}
	if len(data) > bpsMaxImageBytes {
		return "", nil, bpsInvalid("单张图片不能超过 20 MiB")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width <= 0 || config.Height <= 0 {
		return "", nil, bpsInvalid("图片格式无效，或与声明的格式不一致")
	}
	expected := map[string]string{"png": "image/png", "jpeg": "image/jpeg", "gif": "image/gif", "webp": "image/webp"}[format]
	if expected != mime {
		return "", nil, bpsInvalid("图片格式无效，或与声明的格式不一致")
	}
	if int64(config.Width)*int64(config.Height) > bpsMaxImagePixels {
		return "", nil, bpsInvalid("图片像素数过大")
	}
	return expected, data, nil
}

func bpsImagePartURL(part map[string]any) string {
	value := part["image_url"]
	if nested, ok := value.(map[string]any); ok {
		value = nested["url"]
	}
	raw, _ := value.(string)
	if strings.HasPrefix(raw, "data:") {
		return raw
	}
	return ""
}

func bpsPictureAccount(account *Account) string {
	return fmt.Sprintf("%d:%s", account.ID, strings.TrimSpace(account.GetCredential("chatgpt_account_id")))
}

func (s *OpenAIGatewayService) bpsUploadPicture(ctx context.Context, account *Account, mime string, data []byte, digest string) (string, bool, error) {
	cache := s.bpsPictureStore()
	key := bpsPictureAccount(account) + ":" + digest
	cache.uploads[int(digest[0])%len(cache.uploads)].Lock()
	defer cache.uploads[int(digest[0])%len(cache.uploads)].Unlock()
	cache.mu.Lock()
	fileID := cache.files[key]
	cache.mu.Unlock()
	if fileID != "" {
		return fileID, true, nil
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	filename := "picture-" + digest[:12] + "." + map[string]string{
		"image/png": "png", "image/jpeg": "jpg", "image/gif": "gif", "image/webp": "webp",
	}[mime]
	headers := make(textproto.MIMEHeader)
	headers.Set("Content-Disposition", `form-data; name="file"; filename="`+filename+`"`)
	headers.Set("Content-Type", mime)
	part, err := writer.CreatePart(headers)
	if err != nil {
		return "", false, &bpsError{502, "bps_image_upload_failed", "无法创建图片上传请求"}
	}
	if _, err = part.Write(data); err != nil {
		return "", false, &bpsError{502, "bps_image_upload_failed", "无法读取图片数据"}
	}
	if err = writer.Close(); err != nil {
		return "", false, &bpsError{502, "bps_image_upload_failed", "无法完成图片上传请求"}
	}
	req, err := buildOpenAIBPSRequest(ctx, account, body.Bytes())
	if err != nil {
		return "", false, err
	}
	req.URL, err = req.URL.Parse(OpenAIBPSAttachmentsURL)
	if err != nil {
		return "", false, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", writer.FormDataContentType())

	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	if s.httpUpstream == nil {
		return "", false, &bpsError{502, "bps_image_upload_failed", "BPS 图片上传服务不可用"}
	}
	resp, err := s.httpUpstream.Do(req, proxyURL, account.ID, account.Concurrency)
	if err != nil {
		return "", false, &bpsError{502, "bps_image_upload_failed", "无法连接 BPS 图片上传服务"}
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, bpsMaxUploadResponse+1))
	if err != nil {
		return "", false, &bpsError{502, "bps_image_upload_failed", "读取图片上传响应失败"}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		status := 502
		if resp.StatusCode == 401 || resp.StatusCode == 403 || resp.StatusCode == 429 {
			status = resp.StatusCode
		}
		return "", false, &bpsError{status, "bps_image_upload_failed", fmt.Sprintf("BPS 图片上传失败（HTTP %d）", resp.StatusCode)}
	}
	if len(raw) > bpsMaxUploadResponse {
		return "", false, &bpsError{502, "bps_image_upload_failed", "BPS 图片上传响应过大"}
	}
	var result struct {
		FileID string `json:"openai_file_id"`
	}
	if json.Unmarshal(raw, &result) != nil || strings.TrimSpace(result.FileID) == "" {
		return "", false, &bpsError{502, "bps_image_upload_failed", "BPS 图片上传服务没有返回附件编号"}
	}
	fileID = strings.TrimSpace(result.FileID)
	cache.mu.Lock()
	if _, exists := cache.files[key]; !exists {
		cache.order = append(cache.order, key)
	}
	cache.files[key] = fileID
	for len(cache.order) > bpsPictureCacheSize {
		evicted := cache.order[0]
		cache.order = cache.order[1:]
		delete(cache.files, evicted)
	}
	cache.mu.Unlock()
	return fileID, false, nil
}

type bpsPictureSent struct {
	inline map[string]bool
	reused map[string]bool
}

func (s *OpenAIGatewayService) bpsForgetPictures(account *Account, ids map[string]bool) {
	cache := s.bpsPictureStore()
	cache.mu.Lock()
	defer cache.mu.Unlock()
	prefix := bpsPictureAccount(account) + ":"
	for key, id := range cache.files {
		if strings.HasPrefix(key, prefix) && ids[id] {
			delete(cache.files, key)
		}
	}
	kept := cache.order[:0]
	for _, key := range cache.order {
		if _, exists := cache.files[key]; exists {
			kept = append(kept, key)
		}
	}
	cache.order = kept
}

func (s *OpenAIGatewayService) bpsRefuseInline(account *Account, kinds map[string]bool) {
	cache := s.bpsPictureStore()
	cache.mu.Lock()
	defer cache.mu.Unlock()
	key := bpsPictureAccount(account)
	if cache.refused[key] == nil {
		cache.refused[key] = make(map[string]bool)
	}
	for kind := range kinds {
		if !cache.refused[key][kind] {
			cache.refusedOrder = append(cache.refusedOrder, key+"\x00"+kind)
		}
		cache.refused[key][kind] = true
	}
	for len(cache.refusedOrder) > bpsPictureCacheSize {
		old := cache.refusedOrder[0]
		cache.refusedOrder = cache.refusedOrder[1:]
		owner, kind, _ := strings.Cut(old, "\x00")
		delete(cache.refused[owner], kind)
		if len(cache.refused[owner]) == 0 {
			delete(cache.refused, owner)
		}
	}
}

func (s *OpenAIGatewayService) prepareOpenAIBPSImages(ctx context.Context, account *Account, body []byte, fresh map[string]bool) ([]byte, bpsPictureSent, error) {
	sent := bpsPictureSent{inline: make(map[string]bool), reused: make(map[string]bool)}
	var source map[string]any
	if err := bpsDecode(body, &source); err != nil || source == nil {
		return nil, sent, bpsInvalid("Invalid Responses request")
	}
	items, ok := source["input"].([]any)
	if !ok {
		return body, sent, nil
	}
	decoded := make(map[string]struct {
		mime string
		data []byte
		id   string
	})
	total := 0
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		field := "content"
		if kind := stringValue(item["type"]); kind == "function_call_output" || kind == "custom_tool_call_output" {
			field = "output"
		}
		parts, ok := item[field].([]any)
		if !ok {
			continue
		}
		for _, rawPart := range parts {
			part, ok := rawPart.(map[string]any)
			if !ok || part["type"] != "input_image" {
				continue
			}
			url := bpsImagePartURL(part)
			if url == "" {
				continue
			}
			entry, exists := decoded[url]
			if !exists {
				mime, data, err := bpsDecodeImageDataURL(url)
				if err != nil {
					return nil, sent, err
				}
				entry = struct {
					mime string
					data []byte
					id   string
				}{mime: mime, data: data}
				decoded[url] = entry
			}
			total += len(entry.data)
			if total > bpsMaxRequestImageBytes {
				return nil, sent, bpsInvalid("请求中的图片合计超过 32 MiB")
			}
		}
	}
	if len(decoded) == 0 {
		return body, sent, nil
	}
	cache := s.bpsPictureStore()
	cache.mu.Lock()
	refused := make(map[string]bool)
	for kind := range cache.refused[bpsPictureAccount(account)] {
		refused[kind] = true
	}
	cache.mu.Unlock()
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		field := "content"
		kind := stringValue(item["type"])
		if kind == "" {
			kind = "message"
		}
		if kind == "function_call_output" || kind == "custom_tool_call_output" {
			field = "output"
		}
		parts, ok := item[field].([]any)
		if !ok {
			continue
		}
		for i, rawPart := range parts {
			part, ok := rawPart.(map[string]any)
			if !ok || part["type"] != "input_image" {
				continue
			}
			if entry, exists := decoded[bpsImagePartURL(part)]; exists {
				if !refused[kind] {
					sent.inline[kind] = true
					continue
				}
				if entry.id == "" {
					sum := sha256.Sum256(entry.data)
					id, reused, err := s.bpsUploadPicture(ctx, account, entry.mime, entry.data, hex.EncodeToString(sum[:]))
					if err != nil {
						return nil, sent, err
					}
					entry.id = id
					decoded[bpsImagePartURL(part)] = entry
					if reused && !fresh[id] {
						sent.reused[id] = true
					} else {
						fresh[id] = true
					}
				}
				replacement := make(map[string]any, len(part)+1)
				for key, value := range part {
					if key != "image_url" {
						replacement[key] = value
					}
				}
				replacement["file_id"] = entry.id
				if _, exists := replacement["detail"]; !exists {
					replacement["detail"] = "auto"
				}
				parts[i] = replacement
			}
		}
	}
	rewritten, err := json.Marshal(source)
	return rewritten, sent, err
}
