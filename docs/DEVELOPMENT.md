# Hướng dẫn tiếp tục phát triển VivuRouter Go

Tài liệu này dành cho AI hoặc developer tiếp tục phát triển bản mẫu Go trong [`vivurouter-go`](../).

## Quy tắc làm việc

1. Chỉ chỉnh file trong [`vivurouter-go/`](../) trừ khi có yêu cầu rõ ràng khác.
2. Không chỉnh dự án Node.js/Next.js gốc.
3. Sau khi sửa Go code, chạy:

```powershell
gofmt -w .\cmd .\internal
go test ./...
go build ./cmd/vivurouter-go
```

4. Nếu thêm endpoint hoặc behavior mới, cập nhật [`README.md`](../README.md) và tài liệu này.
5. Không ghi token/API key thật vào git.

## Cách chạy local

```powershell
cd vivurouter-go
go run ./cmd/vivurouter-go
```

Dashboard:

```text
http://127.0.0.1:20129/dashboard
```

Smoke test:

```powershell
.\scripts\smoke.ps1
```

## Quy trình thêm provider mới

Ví dụ muốn thêm provider `example` dạng OpenAI-compatible đặc thù.

### Bước 1: Khai báo provider type nếu cần

Sửa [`store.go`](../internal/store/store.go):

```go
const ProviderExample = "example"
```

Nếu provider chỉ là OpenAI-compatible thông thường, có thể dùng `ProviderOpenAICompatible` hiện có.

### Bước 2: Tạo executor

Tạo file mới trong [`internal/provider/`](../internal/provider/), ví dụ `example.go`:

```go
package provider

import (
    "context"
    "net/http"

    "github.com/local/vivurouter-go/internal/store"
)

type ExampleExecutor struct {
    Client *http.Client
}

func (e *ExampleExecutor) ExecuteChat(ctx context.Context, provider store.Provider, model string, body map[string]any) (*ExecuteResult, error) {
    // Build request, set headers, call upstream.
    return nil, nil
}
```

### Bước 3: Wire executor

Sửa [`executor.go`](../internal/provider/executor.go):

- Thêm field vào `Executors`.
- Khởi tạo trong `NewExecutors()`.

### Bước 4: Dispatch trong gateway

Sửa [`handler.go`](../internal/gateway/handler.go) trong `ChatCompletions()`:

- Nếu `resolved.Provider.Type == store.ProviderExample`, gọi executor mới.
- Trả JSON/SSE tương tự OpenAI executor.

### Bước 5: Dashboard

Sửa [`providers.html`](../web/templates/providers.html), thêm option provider type mới vào `<select>`.

### Bước 6: Test

- Thêm seed provider hoặc nhập bằng dashboard.
- Gọi `GET /v1/models`.
- Gọi `POST /v1/chat/completions` non-stream và stream.

## Quy trình thêm endpoint mới

Ví dụ thêm `/v1/embeddings`.

### Bước 1: Tạo handler

Có thể thêm method vào [`handler.go`](../internal/gateway/handler.go) hoặc tạo file riêng `handler_embeddings.go` trong [`internal/gateway/`](../internal/gateway/).

Pattern nên giữ:

1. Check method bằng `methodAllowed`.
2. Load settings/providers bằng `loadGatewayState` nếu cần API key/provider.
3. Parse JSON bằng `readJSONBody`.
4. Resolve provider/model.
5. Gọi executor.
6. Ghi request log bằng `logRequest` hoặc helper riêng.

### Bước 2: Đăng ký route

Sửa [`routes.go`](../internal/app/routes.go):

```go
mux.HandleFunc("/v1/embeddings", gw.Embeddings)
```

### Bước 3: Thêm executor method

Nếu endpoint không dùng chat, thêm interface/method mới trong [`executor.go`](../internal/provider/executor.go), ví dụ:

```go
type EmbeddingsExecutor interface {
    ExecuteEmbeddings(ctx context.Context, provider store.Provider, model string, body map[string]any) (*ExecuteResult, error)
}
```

### Bước 4: Cập nhật docs và smoke

- README thêm endpoint.
- [`scripts/smoke.ps1`](../scripts/smoke.ps1) thêm request mẫu nếu không cần token thật.

## Quy trình mở rộng translator

Hiện có translator chính:

- [`ChatToResponses()`](../internal/translator/chat_to_responses.go)
- Placeholder [`ResponsesToChat()`](../internal/translator/responses_to_chat.go)
- Streaming Responses → Chat nằm trong [`streamResponsesToChat()`](../internal/gateway/sse.go)

### Khi mở rộng Chat → Responses

Sửa [`chat_to_responses.go`](../internal/translator/chat_to_responses.go) để hỗ trợ thêm:

- `developer` message.
- `reasoning` content.
- Hosted tools.
- `tool_choice`.
- `response_format` mapping.
- Multimodal input đầy đủ.

Nên giữ nguyên nguyên tắc:

- Không mutate input map nếu không cần.
- Chấp nhận `map[string]any` để linh hoạt với payload LLM.
- Test bằng fixture JSON nhỏ.

### Khi mở rộng Responses SSE → Chat SSE

Sửa [`sse.go`](../internal/gateway/sse.go), hàm `streamResponsesToChat`.

Event đang hỗ trợ:

- `response.output_text.delta`
- `response.reasoning_summary_text.delta`
- `response.output_item.added` cho function call
- `response.function_call_arguments.delta`
- `response.custom_tool_call_input.delta`
- `response.output_item.done`
- `response.completed`
- `response.done`
- `error`
- `response.failed`

Khi thêm event:

1. Parse event type.
2. Emit Chat Completions chunk theo schema OpenAI.
3. Không giữ buffer lớn không cần thiết.
4. Đảm bảo final chunk và `[DONE]` chỉ gửi một lần.

## Quy trình thay FileStore bằng SQLite

Hiện [`FileStore`](../internal/store/file.go) tiện cho mẫu nhỏ, nhưng production nên dùng SQLite.

### Bước 1: Giữ interface

Không đổi [`Store`](../internal/store/store.go) nếu chưa cần.

### Bước 2: Tạo `sqlite.go`

Tạo [`internal/store/sqlite.go`](../internal/store/sqlite.go) implement:

- `GetSettings`
- `SaveSettings`
- `ListProviders`
- `GetProvider`
- `UpsertProvider`
- `DeleteProvider`
- `AddRequestLog`
- `RecentRequestLogs`

### Bước 3: Schema tối thiểu

Gợi ý:

```sql
CREATE TABLE settings (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  data TEXT NOT NULL
);

CREATE TABLE providers (
  id TEXT PRIMARY KEY,
  type TEXT NOT NULL,
  name TEXT,
  base_url TEXT,
  api_key TEXT,
  access_token TEXT,
  refresh_token TEXT,
  enabled INTEGER NOT NULL DEFAULT 1,
  models TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE request_logs (
  id TEXT PRIMARY KEY,
  timestamp TEXT NOT NULL,
  endpoint TEXT,
  provider_id TEXT,
  model TEXT,
  status TEXT,
  duration_ms INTEGER,
  stream INTEGER,
  error TEXT
);
```

### Bước 4: Chọn backend store bằng env

Thêm env trong [`config.go`](../internal/config/config.go):

```text
STORE_BACKEND=file|sqlite
```

Sửa [`main.go`](../cmd/vivurouter-go/main.go) để chọn `NewFileStore` hoặc `NewSQLiteStore`.

## Quy trình kiểm tra Codex OAuth onboarding

Codex OAuth đã có helper riêng tại [`internal/codexoauth/codex.go`](../internal/codexoauth/codex.go) và UI trong [`providers.html`](../web/templates/providers.html).

Luồng cần giữ tương thích với Codex CLI:

1. `POST /api/codex/oauth/start` tạo `state`, `code_verifier`, `code_challenge` PKCE S256.
2. Callback server mở cố định `http://localhost:1455/auth/callback`.
3. Auth URL dùng `client_id=app_EMoamEEZ73f0CkXaXp7hrann`, scope `openid profile email offline_access`, `id_token_add_organizations=true`, `codex_cli_simplified_flow=true`, `originator=codex_cli_rs`.
4. Callback exchange code tại `https://auth.openai.com/oauth/token` bằng form `application/x-www-form-urlencoded`.
5. Token được lưu bằng `Store.UpsertProvider` vào provider type `codex`.

Unit test nên kiểm tra ít nhất auth URL, PKCE và token exchange form như [`codex_test.go`](../internal/codexoauth/codex_test.go).

## Quy trình thêm token refresh Codex

Codex OAuth onboarding đã lưu `access_token` và `refresh_token`. [`CodexExecutor`](../internal/provider/codex.go) hiện đã tự refresh khi upstream trả 401/403, lưu token mới vào store rồi retry request một lần. Phần còn thiếu là proactive refresh theo thời điểm hết hạn vì [`Provider`](../internal/store/store.go) chưa lưu `expires_at`.

Nếu muốn nâng cấp refresh chủ động theo hạn token:

1. Thêm expires fields vào [`Provider`](../internal/store/store.go), ví dụ `ExpiresAt`.
2. Persist `expires_at` khi OAuth exchange/refresh trả `expires_in`.
3. Trước khi gọi upstream, kiểm tra token sắp hết hạn.
4. Tránh nhiều request refresh cùng lúc bằng mutex theo provider ID.
5. Giữ fallback hiện có: nếu upstream vẫn trả 401/403, gọi refresh endpoint OpenAI OAuth, cập nhật provider qua `UpsertProvider`, rồi retry một lần.

Lưu ý:

- Không log refresh token.
- Dùng context timeout riêng cho refresh nếu tách khỏi request context chính.
- Tránh nhiều request refresh cùng lúc bằng mutex theo provider ID trước khi thêm proactive refresh.

## Quy trình thêm fallback account/provider

Hiện mỗi model resolve về một provider duy nhất.

Để thêm fallback:

1. Cho phép nhiều provider cùng type hoặc cùng tag model.
2. Sửa [`resolveModel()`](../internal/gateway/router.go) trả danh sách candidate thay vì một item.
3. Trong handler, lặp qua candidates.
4. Nếu lỗi fallback-eligible như 429/5xx/network, thử candidate tiếp theo.
5. Lưu cooldown vào store, ví dụ `rate_limited_until` hoặc `model_locks`.
6. Dashboard hiển thị trạng thái cooldown.

## Quy trình thêm combo model

Combo có thể thêm sau khi fallback provider ổn định.

Gợi ý model:

```json
{
  "name": "fast-combo",
  "models": ["openai/gpt-4o-mini", "codex/gpt-5-codex"],
  "strategy": "fallback"
}
```

Cần thêm:

- Struct `Combo` trong store.
- CRUD combo trong dashboard/API.
- `resolveModel()` kiểm tra combo name trước provider/model.
- Handler thử từng model trong combo.

## Quy trình thêm test

Đã có test cho store, gateway usage/fallback và Codex OAuth. Khi mở rộng, tiếp tục thêm unit test nhỏ cho translator, router và provider.

Gợi ý file:

- `internal/translator/chat_to_responses_test.go`
- `internal/gateway/router_test.go`
- `internal/provider/codex_test.go`

Test nên bao gồm:

- Chat text đơn giản sang Responses.
- System message sang instructions.
- Tool call và tool output.
- Model resolve `provider/model`.
- Model resolve default provider.
- Codex unsupported fields bị strip.

Chạy:

```powershell
go test ./...
```

## Checklist trước khi hoàn tất một thay đổi

- [ ] Code đã `gofmt`.
- [ ] `go test ./...` pass.
- [ ] `go build ./cmd/vivurouter-go` pass.
- [ ] README cập nhật nếu thay đổi endpoint/config.
- [ ] `docs/ARCHITECTURE.md` cập nhật nếu thay đổi luồng hoặc cấu trúc.
- [ ] Không log hoặc commit token thật.
- [ ] Không chỉnh dự án VivuRouter gốc nếu không được yêu cầu.

## Đã triển khai gần đây

- **SQLite store tùy chọn**: [`internal/store/sqlite.go`](../internal/store/sqlite.go), chọn bằng `STORE_BACKEND=file|sqlite`. Dùng `modernc.org/sqlite` (pure-Go, không cần cgo/build tools). Seed và normalize giống FileStore nên test CRUD dùng chung chạy cho cả hai backend.
- **Fallback nhiều provider/account**: [`resolveCandidates()`](../internal/gateway/router.go) trả danh sách candidate; [`runWithFallback()`](../internal/gateway/fallback.go) thử lần lượt, retry tại chỗ 1 lần cho lỗi kết nối, và bỏ qua provider đang cooldown. Lỗi fallback-eligible = network / 429 / 5xx. 4xx khác được passthrough cho client, không fallback.
- **Cooldown**: [`internal/observe/cooldown.go`](../internal/observe/cooldown.go), in-memory, đọc `Retry-After` cho 429. **Lưu ý**: không persist qua restart — phù hợp sample; nếu cần bền vững thì lưu vào store (`rate_limited_until`).
- **Observability**: [`internal/observe/metrics.go`](../internal/observe/metrics.go) đếm in-flight/total/status/upstream-fail; `GET /api/metrics`, `GET /api/cooldowns`, panel Runtime trên dashboard; `pprof` mở khi `DEBUG=true`.
- **Usage/cost tracking tối thiểu**: [`internal/gateway/usage.go`](../internal/gateway/usage.go) extract usage từ OpenAI JSON/SSE và Responses SSE, estimate token khi upstream không trả usage, tính cost theo pricing mặc định hoặc env `USAGE_PRICE_*_PER_1M`; [`RequestLog`](../internal/store/store.go) đã có token/cost fields; dashboard có panel Usage & Cost và API `GET /api/usage/stats`, `GET /api/usage/recent`.
- **Codex OAuth onboarding**: [`internal/codexoauth/codex.go`](../internal/codexoauth/codex.go) tạo auth URL giống Codex CLI, mở callback `http://localhost:1455/auth/callback`, exchange code tại OpenAI OAuth token endpoint và lưu access/refresh token vào provider `codex`; dashboard Providers có nút kết nối và API `POST /api/codex/oauth/start`, `GET /api/codex/oauth/status`.
- **Codex token refresh khi 401/403**: [`CodexExecutor`](../internal/provider/codex.go) gọi refresh-token grant, lưu access/refresh token mới vào store và retry request một lần; unit test nằm ở [`internal/provider/codex_test.go`](../internal/provider/codex_test.go).
- **Graceful shutdown**: [`cmd/vivurouter-go/main.go`](../cmd/vivurouter-go/main.go) bắt SIGINT/SIGTERM, `server.Shutdown` với `SHUTDOWN_TIMEOUT`. Server set `ReadHeaderTimeout`/`IdleTimeout`, **không** set `WriteTimeout` để không cắt SSE.
- **Dashboard CRUD**: providers có nút Sửa (`?edit=<id>`) và Xóa (POST `action=delete`), settings chọn default provider bằng `<select>` và sửa `keep_request_logs`.

## Ưu tiên phát triển tiếp theo

1. Persist cooldown vào store để bền vững qua restart.
2. Thêm proactive Codex refresh theo `expires_at`.
3. Port thêm endpoint `/v1/embeddings` nếu cần.
4. Mở rộng Responses SSE → Chat SSE theo test fixture thực tế.
5. Nâng cấp usage/cost tracking: pricing CRUD theo provider/model, biểu đồ theo thời gian, quota/budget alert.
6. Thêm latency histogram vào metrics.
7. Combo model fallback (xem mục "Quy trình thêm combo model").
