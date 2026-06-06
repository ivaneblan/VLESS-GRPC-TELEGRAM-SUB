package botapp

import (
	"fmt"
	"strconv"
)

type Request struct {
	RequestID string `yaml:"request_id"`
	UserID    int64  `yaml:"user_id"`
	Username  string `yaml:"username"`
	FirstName string `yaml:"first_name"`
	Status    string `yaml:"status"`
	CreatedAt int64  `yaml:"created_at"`
}

func (r Request) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"request_id": r.RequestID,
		"user_id":    r.UserID,
		"username":   r.Username,
		"first_name": r.FirstName,
		"status":     r.Status,
		"created_at": r.CreatedAt,
	}
}

func ParseRequest(id string, raw interface{}) (Request, error) {
	m, ok := raw.(map[string]interface{})
	if !ok {
		return Request{}, fmt.Errorf("invalid request %s", id)
	}
	req := Request{RequestID: id}
	if v, ok := m["request_id"].(string); ok && v != "" {
		req.RequestID = v
	}
	switch v := m["user_id"].(type) {
	case int:
		req.UserID = int64(v)
	case int64:
		req.UserID = v
	case float64:
		req.UserID = int64(v)
	}
	if v, ok := m["username"].(string); ok {
		req.Username = v
	}
	if v, ok := m["first_name"].(string); ok {
		req.FirstName = v
	}
	if v, ok := m["status"].(string); ok {
		req.Status = v
	}
	switch v := m["created_at"].(type) {
	case int:
		req.CreatedAt = int64(v)
	case int64:
		req.CreatedAt = v
	case float64:
		req.CreatedAt = int64(v)
	}
	return req, nil
}

func RequestStatus(raw interface{}) string {
	m, ok := raw.(map[string]interface{})
	if !ok {
		return ""
	}
	if v, ok := m["status"].(string); ok {
		return v
	}
	return ""
}

func RequestCreatedAt(raw interface{}) int64 {
	m, ok := raw.(map[string]interface{})
	if !ok {
		return 0
	}
	switch v := m["created_at"].(type) {
	case int:
		return int64(v)
	case int64:
		return v
	case float64:
		return int64(v)
	default:
		return 0
	}
}

func parseUserID(s string) (int64, bool) {
	id, err := strconv.ParseInt(s, 10, 64)
	return id, err == nil
}
