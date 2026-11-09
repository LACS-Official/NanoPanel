package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// maxBodyBytes 限制 JSON 请求体大小，防止超大请求消耗内存。
const maxBodyBytes = 8 << 10

// Response 统一响应结构。
type Response struct {
	OK   bool        `json:"ok"`
	Data interface{} `json:"data,omitempty"`
	Err  string      `json:"err,omitempty"`
}

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func ok(w http.ResponseWriter, data interface{}) {
	writeJSON(w, http.StatusOK, Response{OK: true, Data: data})
}

func fail(w http.ResponseWriter, code int, err string) {
	writeJSON(w, code, Response{OK: false, Err: err})
}

// decodeJSON 读取并解析受限大小的 JSON 请求体。
func decodeJSON(w http.ResponseWriter, r *http.Request, dst interface{}) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return fmt.Errorf("请求体过大")
		}
		return fmt.Errorf("请求格式错误")
	}
	// 拒绝尾部附加的多余 JSON 值。
	var extra interface{}
	if err := dec.Decode(&extra); err == nil {
		return fmt.Errorf("请求格式错误")
	}
	return nil
}
