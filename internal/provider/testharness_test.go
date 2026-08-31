package provider

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

// mockServer 는 실제 API 의 응답 모양(ApiResponse 봉투, problem+json, "삭제돼도
// 단건 GET 은 200")을 흉내내는 리소스 저장소다. 전이는 GET 한 번에 한 칸씩 진행된다.
type mockServer struct {
	t   *testing.T
	srv *httptest.Server

	mu             sync.Mutex
	objects        map[string]map[string]map[string]any // base -> id -> object
	transitions    map[string][]string                  // id -> 남은 상태열 (마지막 값 유지)
	createStatuses map[string][][]string                // base -> 다음 POST 들에 줄 상태열
	createExtras   map[string][]map[string]any          // base -> 다음 POST 응답 data 에 덧붙일 필드
	deleteStatuses map[string][]string                  // base -> DELETE 후 상태열
	requests       []recordedRequest
	failures       []plannedFailure
}

type recordedRequest struct {
	Method string
	Path   string
	Body   map[string]any
}

type plannedFailure struct {
	method      string
	pathSuffix  string
	status      int
	problemType string
	data        map[string]any
}

func newMockServer(t *testing.T) *mockServer {
	ms := &mockServer{
		t:              t,
		objects:        map[string]map[string]map[string]any{},
		transitions:    map[string][]string{},
		createStatuses: map[string][][]string{},
		createExtras:   map[string][]map[string]any{},
		deleteStatuses: map[string][]string{},
	}
	ms.srv = httptest.NewServer(http.HandlerFunc(ms.handle))
	t.Cleanup(ms.srv.Close)
	return ms
}

func (ms *mockServer) URL() string { return ms.srv.URL }

// register 는 base 경로(예 /v1/network/virtual-networks)의 CRUD 를 연다.
func (ms *mockServer) register(base string, deleteStatuses ...string) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	if ms.objects[base] == nil {
		ms.objects[base] = map[string]map[string]any{}
	}
	if len(deleteStatuses) == 0 {
		deleteStatuses = []string{"deleted"}
	}
	ms.deleteStatuses[base] = deleteStatuses
}

// createStatus 는 base 의 다음 POST 가 만들 리소스의 상태열을 정한다.
func (ms *mockServer) createStatus(base string, statuses ...string) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	ms.createStatuses[base] = append(ms.createStatuses[base], statuses)
}

// createResponseExtra 는 base 의 다음 POST 응답 data 에 필드를 덧붙인다 —
// 발급 응답에만 실리는 값(object storage user 의 secret 등)을 흉내낸다.
// 저장된 오브젝트에는 넣지 않으므로 이후 GET 에는 나오지 않는다.
func (ms *mockServer) createResponseExtra(base string, extra map[string]any) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	ms.createExtras[base] = append(ms.createExtras[base], extra)
}

// seed 는 이미 존재하는 리소스를 심는다. 반환값은 id.
func (ms *mockServer) seed(base string, obj map[string]any, statuses ...string) string {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	id := uuid.NewString()
	copied := map[string]any{"id": id}
	for k, v := range obj {
		copied[k] = v
	}
	if len(statuses) == 0 {
		statuses = []string{"active"}
	}
	copied["status"] = statuses[0]
	ms.objects[base][id] = copied
	ms.transitions[id] = statuses
	return id
}

// setStatus 는 리소스 상태를 즉시 바꾼다 (disappear 시나리오).
func (ms *mockServer) setStatus(base, id string, statuses ...string) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	ms.transitions[id] = statuses
	ms.objects[base][id]["status"] = statuses[0]
}

// failWith 는 method+경로 접미사가 일치하는 다음 1회 호출을 problem+json 으로 실패시킨다.
func (ms *mockServer) failWith(method, pathSuffix string, status int, problemType string, data map[string]any) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	ms.failures = append(ms.failures, plannedFailure{method, pathSuffix, status, problemType, data})
}

// lastBody 는 method+경로 접미사가 일치하는 마지막 요청의 바디를 돌려준다.
func (ms *mockServer) lastBody(method, pathSuffix string) map[string]any {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	for i := len(ms.requests) - 1; i >= 0; i-- {
		r := ms.requests[i]
		if r.Method == method && strings.HasSuffix(r.Path, pathSuffix) {
			return r.Body
		}
	}
	return nil
}

func (ms *mockServer) countRequests(method, pathSuffix string) int {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	n := 0
	for _, r := range ms.requests {
		if r.Method == method && strings.HasSuffix(r.Path, pathSuffix) {
			n++
		}
	}
	return n
}

func (ms *mockServer) object(base, id string) map[string]any {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	return ms.objects[base][id]
}

func (ms *mockServer) handle(w http.ResponseWriter, r *http.Request) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	var body map[string]any
	if raw, _ := io.ReadAll(r.Body); len(raw) > 0 {
		_ = json.Unmarshal(raw, &body)
	}
	ms.requests = append(ms.requests, recordedRequest{r.Method, r.URL.Path, body})

	for i, f := range ms.failures {
		if f.method == r.Method && strings.HasSuffix(r.URL.Path, f.pathSuffix) {
			ms.failures = append(ms.failures[:i], ms.failures[i+1:]...)
			writeProblem(w, f.status, f.problemType, f.data)
			return
		}
	}

	base, id := ms.split(r.URL.Path)
	if base == "" {
		writeProblem(w, 404, "urn:packetstream:problem:not-found", nil)
		return
	}

	switch {
	case r.Method == http.MethodPost && id == "":
		newID := uuid.NewString()
		obj := map[string]any{"id": newID}
		for k, v := range body {
			obj[k] = v
		}
		statuses := []string{"active"}
		if q := ms.createStatuses[base]; len(q) > 0 {
			statuses = q[0]
			ms.createStatuses[base] = q[1:]
		}
		obj["status"] = statuses[0]
		ms.objects[base][newID] = obj
		ms.transitions[newID] = statuses
		created := map[string]any{"id": newID}
		if q := ms.createExtras[base]; len(q) > 0 {
			for k, v := range q[0] {
				created[k] = v
			}
			ms.createExtras[base] = q[1:]
		}
		writeEnvelope(w, 201, created)

	case r.Method == http.MethodGet && id != "":
		obj, ok := ms.objects[base][id]
		if !ok {
			writeProblem(w, 404, "urn:packetstream:problem:not-found", nil)
			return
		}
		if tr := ms.transitions[id]; len(tr) > 1 {
			ms.transitions[id] = tr[1:]
			obj["status"] = tr[1]
		}
		writeEnvelope(w, 200, obj)

	case r.Method == http.MethodPatch && id != "":
		obj, ok := ms.objects[base][id]
		if !ok {
			writeProblem(w, 404, "urn:packetstream:problem:not-found", nil)
			return
		}
		for k, v := range body {
			if v == nil {
				delete(obj, k)
				continue
			}
			obj[k] = v
		}
		writeEnvelope(w, 200, map[string]any{"id": id})

	case r.Method == http.MethodDelete && id != "":
		obj, ok := ms.objects[base][id]
		if !ok {
			writeProblem(w, 404, "urn:packetstream:problem:not-found", nil)
			return
		}
		statuses := ms.deleteStatuses[base]
		ms.transitions[id] = statuses
		obj["status"] = statuses[0]
		writeEnvelope(w, 200, map[string]any{"id": id, "status": statuses[len(statuses)-1]})

	case r.Method == http.MethodGet && id == "":
		items := []map[string]any{}
		for _, obj := range ms.objects[base] {
			items = append(items, obj)
		}
		writeEnvelope(w, 200, map[string]any{
			"content": items, "totalElements": len(items), "totalPages": 1,
			"currentPage": 0, "pageSize": len(items),
		})

	default:
		writeProblem(w, 405, "urn:packetstream:problem:method-not-allowed", nil)
	}
}

func (ms *mockServer) split(path string) (base, id string) {
	for b := range ms.objects {
		if path == b {
			return b, ""
		}
		if strings.HasPrefix(path, b+"/") {
			rest := strings.TrimPrefix(path, b+"/")
			if !strings.Contains(rest, "/") {
				return b, rest
			}
		}
	}
	return "", ""
}

func writeEnvelope(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"success": true, "code": "SUCCESS", "message": "Success", "data": data,
	})
}

func writeProblem(w http.ResponseWriter, status int, problemType string, data map[string]any) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	payload := map[string]any{
		"type": problemType, "title": http.StatusText(status), "status": status, "detail": "mock failure",
	}
	if data != nil {
		payload["packetstreamData"] = data
	}
	_ = json.NewEncoder(w).Encode(payload)
}

func (ms *mockServer) providerConfig() string {
	return fmt.Sprintf(`
provider "neocloud" {
  endpoint = %q
  api_key  = "sk_nc_test"
}
`, ms.URL())
}

func protoV6ProviderFactories() map[string]func() (tfprotov6.ProviderServer, error) {
	return map[string]func() (tfprotov6.ProviderServer, error){
		"neocloud": providerserver.NewProtocol6WithError(New("test")()),
	}
}
