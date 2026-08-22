package main

import (
	"bytes"
	"caption-delivery-qc/internal/application"
	"caption-delivery-qc/internal/httpapi"
	"caption-delivery-qc/internal/journal"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

func listenAddr(flagAddr string) string {
	if flagAddr != "" {
		return flagAddr
	}
	if p := os.Getenv("PORT"); p != "" {
		return "127.0.0.1:" + p
	}
	return "127.0.0.1:19081"
}
func main() {
	addr := flag.String("addr", "", "监听地址")
	self := flag.Bool("selfcheck", false, "执行有界自检")
	flag.Parse()
	resolved := listenAddr(*addr)
	if err := validateAddr(resolved); err != nil {
		fmt.Println("地址无效:", err)
		os.Exit(2)
	}
	st, _ := journal.New("")
	app := application.New(st)
	api := httpapi.New(app)
	srv := &http.Server{Addr: resolved, Handler: api.Handler()}
	if *self {
		if e := runSelfcheck(srv); e != nil {
			fmt.Println("selfcheck失败:", e)
			os.Exit(1)
		}
		fmt.Println("selfcheck通过")
		return
	}
	ln, e := net.Listen("tcp", srv.Addr)
	if e != nil {
		panic(e)
	}
	fmt.Println("captionqc监听", ln.Addr().String())
	if e = srv.Serve(ln); e != nil && e != http.ErrServerClosed {
		panic(e)
	}
}
func runSelfcheck(srv *http.Server) error {
	ln, e := net.Listen("tcp", srv.Addr)
	if e != nil {
		return e
	}
	go srv.Serve(ln)
	defer srv.Close()
	base := "http://" + ln.Addr().String()
	client := &http.Client{Timeout: 3 * time.Second}
	req := func(method, path string, v any, actor string, idempotency string) (map[string]any, error) {
		b, _ := json.Marshal(v)
		r, _ := http.NewRequest(method, base+path, bytes.NewReader(b))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Operator", actor)
		if idempotency != "" {
			r.Header.Set("Idempotency-Key", idempotency)
		}
		resp, e := client.Do(r)
		if e != nil {
			return nil, e
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		var out map[string]any
		_ = json.Unmarshal(data, &out)
		if resp.StatusCode >= 300 {
			return out, fmt.Errorf("%s %s: %s", method, path, string(data))
		}
		return out, nil
	}
	h, e := http.Get(base + "/healthz")
	if e != nil {
		return e
	}
	h.Body.Close()
	j, e := req("POST", "/api/v1/review-jobs", map[string]any{"programTitle": "晨间新闻", "durationMs": 5000, "language": "zh-CN", "deliveryBatch": "batch-1", "ruleSet": "broadcast-v1"}, "producer", "selfcheck-create")
	if e != nil {
		return e
	}
	id, _ := j["id"].(string)
	ver := int64(j["version"].(float64))
	j, e = req("POST", "/api/v1/review-jobs/"+id+"/cues", map[string]any{"cue": map[string]any{"startMs": 0, "endMs": 5000, "speaker": "主持人", "text": "欢迎收听今天的新闻"}, "expectedVersion": ver}, "producer", "")
	if e != nil {
		return e
	}
	ver = int64(j["version"].(float64))
	var cueID string
	if cs, ok := j["cues"].(map[string]any); ok {
		for k := range cs {
			cueID = k
		}
	}
	j, e = req("POST", "/api/v1/review-jobs/"+id+"/submit", map[string]any{"expectedVersion": ver}, "producer", "")
	if e != nil {
		return e
	}
	ver = int64(j["version"].(float64))
	baseRevision := int(j["currentRevision"].(float64))
	baseSnapshot := j["snapshots"].(map[string]any)[fmt.Sprint(baseRevision)].(map[string]any)
	baseDigest := baseSnapshot["contentDigest"].(string)
	j, e = req("POST", "/api/v1/review-jobs/"+id+"/findings", map[string]any{"expectedVersion": ver, "finding": map[string]any{"cueID": cueID, "category": "错别字", "severity": "blocking", "evidence": "示例证据"}}, "reviewer", "")
	if e != nil {
		return e
	}
	ver = int64(j["version"].(float64))
	conclusion, e := req("GET", "/api/v1/review-jobs/"+id+"/findings/finish?reviewRound=1", nil, "reviewer", "")
	if e != nil {
		return e
	}
	j, e = req("POST", "/api/v1/review-jobs/"+id+"/findings/finish", map[string]any{"expectedVersion": ver, "conclusionNote": "存在阻断问题，退回修订", "findingSummaryDigest": conclusion["findingSummaryDigest"]}, "reviewer", "")
	if e != nil {
		return e
	}
	ver = int64(j["version"].(float64))
	var findingID string
	if fs, ok := j["findings"].(map[string]any); ok {
		for k := range fs {
			findingID = k
		}
	}
	j, e = req("POST", "/api/v1/review-jobs/"+id+"/cues", map[string]any{"operations": []map[string]any{{"op": "update", "cueID": cueID, "cue": map[string]any{"id": cueID, "startMs": 0, "endMs": 5000, "speaker": "主持人", "text": "欢迎收听今天的新鲜新闻"}}}, "expectedVersion": ver, "baseRevision": baseRevision, "baseDigest": baseDigest}, "producer", "cue-revision")
	if e != nil {
		return e
	}
	if wrapped, ok := j["job"].(map[string]any); ok {
		j = wrapped
	}
	ver = int64(j["version"].(float64))
	updatedHash := ""
	if cs, ok := j["cues"].(map[string]any); ok {
		if c, ok := cs[cueID].(map[string]any); ok {
			updatedHash, _ = c["contentHash"].(string)
		}
	}
	j, e = req("POST", "/api/v1/review-jobs/"+id+"/findings/response/"+findingID, map[string]any{"expectedVersion": ver, "note": "已修订并复核", "cueContentHash": updatedHash}, "producer", "")
	if e != nil {
		return e
	}
	ver = int64(j["version"].(float64))
	j, e = req("POST", "/api/v1/review-jobs/"+id+"/submit", map[string]any{"expectedVersion": ver}, "producer", "")
	if e != nil {
		return e
	}
	ver = int64(j["version"].(float64))
	conclusion, e = req("GET", "/api/v1/review-jobs/"+id+"/findings/finish?reviewRound=2", nil, "reviewer", "")
	if e != nil {
		return e
	}
	j, e = req("POST", "/api/v1/review-jobs/"+id+"/findings/finish", map[string]any{"expectedVersion": ver, "conclusionNote": "问题已闭环，同意提交批准", "findingSummaryDigest": conclusion["findingSummaryDigest"]}, "reviewer", "")
	if e != nil {
		return e
	}
	ver = int64(j["version"].(float64))
	detail, e := req("GET", "/api/v1/review-jobs/"+id, nil, "quality-lead", "")
	if e != nil {
		return e
	}
	jobObj := detail["job"].(map[string]any)
	rev := int(jobObj["currentRevision"].(float64))
	snap := jobObj["snapshots"].(map[string]any)[fmt.Sprint(rev)].(map[string]any)
	digest := snap["contentDigest"].(string)
	readiness, e := req("GET", "/api/v1/review-jobs/"+id+"/approval/readiness?expectedVersion="+fmt.Sprint(ver)+"&revision="+fmt.Sprint(rev)+"&candidateDigest="+digest, nil, "quality-lead", "")
	if e != nil {
		return e
	}
	j, e = req("POST", "/api/v1/review-jobs/"+id+"/approval", map[string]any{"expectedVersion": ver, "candidateRevision": rev, "candidateDigest": digest, "checklistDigest": readiness["checklistDigest"], "signNote": "已完成质量复核"}, "quality-lead", "")
	if e != nil {
		return e
	}
	ver = int64(j["version"].(float64))
	credential, e := req("POST", "/api/v1/review-jobs/"+id+"/credential", map[string]any{"expectedVersion": ver}, "quality-lead", "selfcheck-credential")
	if e != nil {
		return e
	}
	v, e := req("GET", "/api/v1/review-jobs/"+id+"/verify", nil, "quality-lead", "")
	if e != nil {
		return e
	}
	if ok, _ := v["valid"].(bool); !ok {
		return fmt.Errorf("凭据校验未通过")
	}
	batch, e := req("POST", "/api/v1/verify", map[string]any{"credentials": []any{credential}}, "quality-lead", "")
	if e != nil {
		return e
	}
	if count, _ := batch["validCount"].(float64); count != 1 {
		return fmt.Errorf("批量凭据校验未通过")
	}
	page, e := req("GET", "/api/v1/review-jobs/"+id+"/timeline?limit=2", nil, "quality-lead", "")
	if e != nil {
		return e
	}
	if _, ok := page["events"]; !ok {
		return fmt.Errorf("时间线分页为空")
	}
	return nil
}
func _unused() { _ = strings.TrimSpace }
