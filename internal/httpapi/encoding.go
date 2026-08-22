package httpapi

import (
	"encoding/json"
	"net/http"
)

func decodeStrict(r *http.Request, v any) error {
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	return d.Decode(v)
}
func method(w http.ResponseWriter, r *http.Request, allowed ...string) bool {
	for _, m := range allowed {
		if r.Method == m {
			return true
		}
	}
	w.Header().Set("Allow", allowed[0])
	write(w, http.StatusMethodNotAllowed, map[string]string{"error": "请求方法不支持"})
	return false
}
