package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sort"
)

// externalBatch is how many texts one request to an external API carries.
const externalBatch = 32

// external is an OpenAI-compatible embeddings API: POST {base}/embeddings.
type external struct {
	client              *http.Client
	baseURL, key, model string
}

func (e *external) Embed(ctx context.Context, texts []string) ([][]float32, Usage, error) {
	var out [][]float32
	var used Usage
	for from := 0; from < len(texts); from += externalBatch {
		vs, u, err := e.embed(ctx, texts[from:min(from+externalBatch, len(texts))])
		if err != nil {
			return nil, used, err
		}
		out = append(out, vs...)
		used.Tokens += u.Tokens
	}
	return out, used, nil
}

func (e *external) embed(ctx context.Context, texts []string) ([][]float32, Usage, error) {
	body, _ := json.Marshal(map[string]any{"model": e.model, "input": texts, "encoding_format": "float"})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, Usage{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if e.key != "" {
		req.Header.Set("Authorization", "Bearer "+e.key)
	}
	res, err := e.client.Do(req)
	if err != nil {
		return nil, Usage{}, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 64<<20))
	if err != nil {
		return nil, Usage{}, err
	}
	if res.StatusCode != http.StatusOK {
		msg := string(raw)
		if len(msg) > 300 {
			msg = msg[:300]
		}
		return nil, Usage{}, errorf("embeddings API answered %d: %s", res.StatusCode, msg)
	}
	var got struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
		Usage struct {
			PromptTokens int64 `json:"prompt_tokens"`
			TotalTokens  int64 `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		return nil, Usage{}, errorf("embeddings API answered no embeddings: %v", err)
	}
	if len(got.Data) != len(texts) {
		return nil, Usage{}, errorf("embeddings API answered %d embeddings for %d texts", len(got.Data), len(texts))
	}
	sort.SliceStable(got.Data, func(i, j int) bool { return got.Data[i].Index < got.Data[j].Index })
	out := make([][]float32, len(texts))
	for i, d := range got.Data {
		if len(d.Embedding) == 0 {
			return nil, Usage{}, errorf("embeddings API answered an empty embedding")
		}
		out[i] = normalize(d.Embedding)
	}
	tokens := got.Usage.PromptTokens
	if tokens == 0 {
		tokens = got.Usage.TotalTokens
	}
	return out, Usage{Tokens: tokens}, nil
}
