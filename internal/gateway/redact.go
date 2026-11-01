package gateway

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/uiautodev/flashroute/internal/redact"
)

func (g *Gateway) redactMessages(obj map[string]json.RawMessage) *redact.Redactor {
	if !g.Store.RedactionEnabled() {
		return redact.Compile(nil)
	}
	redactor := redact.CompileWithSecret(g.Store.RedactRules(), g.Store.SessionSecret())
	redactor.StartRecording()
	redactor.Messages(obj)
	g.Store.RecordRedactions(redactor.Hits())
	return redactor
}

func (g *Gateway) redactResponses(obj map[string]json.RawMessage) *redact.Redactor {
	if !g.Store.RedactionEnabled() {
		return redact.Compile(nil)
	}
	redactor := redact.CompileWithSecret(g.Store.RedactRules(), g.Store.SessionSecret())
	redactor.StartRecording()
	for _, key := range []string{"instructions", "input", "tools"} {
		raw, ok := obj[key]
		if !ok {
			continue
		}
		var value any
		if err := json.Unmarshal(raw, &value); err != nil {
			continue
		}
		body, err := json.Marshal(redactor.MaskValue(value))
		if err != nil {
			continue
		}
		obj[key] = body
	}
	g.Store.RecordRedactions(redactor.Hits())
	return redactor
}

func restoreResponsesSSE(src io.ReadCloser, redactor *redact.Redactor) io.ReadCloser {
	pr, pw := io.Pipe()
	go func() {
		defer pw.Close()
		defer src.Close()
		scanner := bufio.NewScanner(src)
		scanner.Buffer(make([]byte, 64<<10), 4<<20)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data:") {
				_, _ = fmt.Fprintf(pw, "%s\n", line)
				continue
			}
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "" || data == "[DONE]" {
				_, _ = fmt.Fprintf(pw, "data: %s\n", data)
				continue
			}
			var value any
			if err := json.Unmarshal([]byte(data), &value); err != nil {
				_, _ = fmt.Fprintf(pw, "data: %s\n", data)
				continue
			}
			body, err := json.Marshal(redactor.RestoreValue(value))
			if err != nil {
				_, _ = fmt.Fprintf(pw, "data: %s\n", data)
				continue
			}
			_, _ = fmt.Fprintf(pw, "data: %s\n", body)
		}
		if err := scanner.Err(); err != nil {
			_ = pw.CloseWithError(err)
		}
	}()
	return pr
}

func restoreSSEBody(src io.ReadCloser, redactor *redact.Redactor) io.ReadCloser {
	pr, pw := io.Pipe()
	go func() {
		defer pw.Close()
		defer src.Close()
		scanner := bufio.NewScanner(src)
		scanner.Buffer(make([]byte, 64<<10), 4<<20)
		stream := redactor.NewStreamRestorer()
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}
			if !strings.HasPrefix(line, "data:") {
				_, _ = fmt.Fprintf(pw, "%s\n\n", line)
				continue
			}
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "[DONE]" {
				if rest := stream.Flush(); rest != "" {
					writeRestoreChunk(pw, rest)
				}
				_, _ = io.WriteString(pw, "data: [DONE]\n\n")
				continue
			}
			var chunk map[string]any
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				_, _ = fmt.Fprintf(pw, "data: %s\n\n", data)
				continue
			}
			restoreChunkContent(chunk, stream)
			body, err := json.Marshal(chunk)
			if err != nil {
				_, _ = fmt.Fprintf(pw, "data: %s\n\n", data)
				continue
			}
			_, _ = fmt.Fprintf(pw, "data: %s\n\n", body)
		}
		if rest := stream.Flush(); rest != "" {
			writeRestoreChunk(pw, rest)
		}
		if err := scanner.Err(); err != nil {
			_ = pw.CloseWithError(err)
		}
	}()
	return pr
}

func restoreChunkContent(chunk map[string]any, stream *redact.StreamRestorer) {
	choices, _ := chunk["choices"].([]any)
	for _, rawChoice := range choices {
		choice, _ := rawChoice.(map[string]any)
		if choice == nil {
			continue
		}
		for _, key := range []string{"delta", "message"} {
			container, _ := choice[key].(map[string]any)
			if container == nil {
				continue
			}
			content, _ := container["content"].(string)
			if content != "" {
				container["content"] = stream.Push(content)
			}
			for _, key := range []string{"reasoning_content", "reasoning"} {
				reasoning, _ := container[key].(string)
				if reasoning != "" {
					container[key] = stream.Push(reasoning)
				}
			}
		}
	}
}

func writeRestoreChunk(w io.Writer, content string) {
	chunk := map[string]any{
		"choices": []map[string]any{{
			"index": 0,
			"delta": map[string]any{"content": content},
		}},
	}
	body, err := json.Marshal(chunk)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(w, "data: %s\n\n", body)
}
