package web

import (
  "encoding/json"
  "net/http"
  "net/http/httptest"
  "testing"

  "github.com/gin-gonic/gin"
)

func TestWriteJSON(t *testing.T) {
  w := httptest.NewRecorder()
  c, _ := gin.CreateTestContext(w)
  writeJSON(c, http.StatusOK, map[string]interface{}{"key": "value"})

  if w.Code != http.StatusOK {
    t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
  }
  var body map[string]interface{}
  if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
    t.Fatalf("json unmarshal: %v", err)
  }
  if body["key"] != "value" {
    t.Errorf("body = %v", body)
  }
}

func TestWriteSuccess(t *testing.T) {
  w := httptest.NewRecorder()
  c, _ := gin.CreateTestContext(w)
  writeSuccess(c, "操作成功")

  if w.Code != http.StatusOK {
    t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
  }
  var body map[string]interface{}
  if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
    t.Fatalf("json unmarshal: %v", err)
  }
  if body["message"] != "操作成功" {
    t.Errorf("message = %v", body["message"])
  }
  if body["success"] != true {
    t.Errorf("success = %v", body["success"])
  }
}

func TestWriteError(t *testing.T) {
  w := httptest.NewRecorder()
  c, _ := gin.CreateTestContext(w)
  writeError(c, http.StatusBadRequest, "参数错误")

  if w.Code != http.StatusBadRequest {
    t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
  }
  var body map[string]interface{}
  if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
    t.Fatalf("json unmarshal: %v", err)
  }
  if body["error"] != "参数错误" {
    t.Errorf("error = %v", body["error"])
  }
}
