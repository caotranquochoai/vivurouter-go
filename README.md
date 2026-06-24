# VivuRouter Go

VivuRouter Go là một local AI gateway hiệu năng cao và dashboard quản trị toàn diện được viết bằng ngôn ngữ Go. Dự án được thiết kế để chạy trực tiếp trên máy cá nhân hoặc máy chủ local, giúp gom các tài khoản AI (OpenAI-compatible, ChatGPT Codex, Google Cloud Code Assist OAuth, free mirrors) thành một cổng API duy nhất tương thích OpenAI & Anthropic Messages, phục vụ cho các công cụ phát triển như **Claude Code**, **RooCode**, **Cursor**, v.v.

Dự án ưu tiên vận hành local, dễ build thành một binary duy nhất (single binary), dễ backup/restore, bảo mật thông tin và tối ưu hóa chi phí.

---

## 🚀 Tính năng nổi bật

### 1. Unified Gateway API (Tương thích OpenAI & Anthropic)
- **Tương thích OpenAI**: Hỗ trợ đầy đủ `GET /v1/models` và `POST /v1/chat/completions` (cả stream và non-stream).
- **Tương thích Anthropic Messages (Claude Code)**: Endpoint `POST /v1/messages` cho phép trỏ trực tiếp các công cụ như `Claude Code` / `Claude CLI` thông qua biến môi trường `ANTHROPIC_BASE_URL`.
- **Tự động Translator**:
  - Dịch Anthropic Messages sang định dạng OpenAI Chat Completions.
  - Dịch Anthropic Messages sang trực tiếp Codex Responses để tối ưu hóa token cache và giữ nguyên cấu trúc `system`, `developer`, `tool_use`, `tool_result`.
  - Chuyển đổi định dạng Stream OpenAI/Codex ngược lại thành Anthropic SSE events (`message_start`, `content_block_delta`, v.v.) chuẩn chỉ giúp Claude Code chạy tool mượt mà.
  - Sanitize tham số tool gọi từ Claude Code (như giới hạn file read `limit`, offset âm, filter PDF pages) để giảm thiểu lỗi schema.

### 2. Multi-Provider & Key Rotation
- **OpenAI-Compatible**: Quản lý nhiều Endpoint/Provider khác nhau (OpenAI, DeepSeek, Anthropic, Ollama, OpenRouter...).
- **Chat Completions / Codex Bridge**: Tương thích hoàn toàn với luồng ChatGPT backend API, hỗ trợ OAuth Access Token tự động refresh qua PKCE S256 Flow hoặc nhập thủ công. Tự tăng ID dạng `codex-2`, `codex-3` để không ghi đè account cũ.
- **Provider Free-Tier tích hợp sẵn**:
  - **MiMo Code Free**: Sử dụng cơ chế bootstrap JWT nội bộ.
  - **OpenCode Free**: Sử dụng Bearer public không cần cấu hình.
- **Antigravity**: Tích hợp Google Cloud Code Assist OAuth API (`daily-cloudcode-pa.googleapis.com`) hỗ trợ token rotation và quota API.
- **Key Rotation**: Một Provider có thể cấu hình nhiều API Key khác nhau chạy theo chiến lược `fill-first` hoặc `round-robin` với hạn mức `sticky_limit` động.

### 3. Proxy Pool & Thiết kế Fail-Closed Bảo Mật
- Hỗ trợ đầy đủ các loại proxy: `http://`, `https://`, `socks5://`, `socks5h://` kèm basic auth (`user:pass`).
- Có nút kiểm tra kết nối proxy trước khi lưu.
- **Cơ chế Fail-Closed**: Khi một Provider được cấu hình dùng proxy, nếu proxy lỗi hoặc chết, request sẽ lập tức bị hủy bỏ và báo lỗi. Hệ thống tuyệt đối **không tự động fallback về IP gốc của máy chủ** nhằm đảm bảo không bao giờ bị rò rỉ địa chỉ IP thật của bạn lên upstream provider.

### 4. Combo Model (Virtual Routing)
- Cho phép tạo các model ảo (ví dụ: `my-fast-model`) đại diện cho một danh sách các model thật.
- Các chiến lược định tuyến:
  - `fallback`: Tự động thử model tiếp theo trong danh sách nếu model trước bị lỗi mạng, rate-limit (429), hoặc lỗi server (5xx). Hỗ trợ Cooldown in-memory & đọc `Retry-After` để tạm dừng gửi request đến provider bị lỗi.
  - `round-robin`: Chia đều tải giữa các model thành viên.
- Giao diện Combo Builder trực quan tại `/combos` hỗ trợ lọc và chọn model nhanh bằng chip UI thay vì phải nhập text thủ công.

### 5. Prompt Router (Định tuyến thông minh)
- Đánh giá prompt đầu vào thông qua một classifier model siêu tốc.
- Phân loại prompt theo Role (ví dụ: `coding`, `writing`), độ phức tạp (`low`, `medium`, `high`) và độ rủi ro (`low`, `medium`, `high`).
- Định tuyến động request đến model phù hợp nhất dựa trên kết quả phân tích.
- Tự động nén/cắt giảm template prompt hệ thống hoặc tool schemas tùy biến để tiết kiệm token.

### 6. Multi-Agent Fusion Engine (Hợp nhất phản hồi)
- Phân rã request của người dùng gửi đồng thời (`parallel`) hoặc tuần tự (`sequential`) tới nhiều expert model khác nhau.
- Gom toàn bộ kết quả thảo luận từ các expert đưa qua một bộ tổng hợp (**Synthesizer**).
- Tùy chọn chạy qua bộ kiểm duyệt phản hồi (**Reviewer**) để có câu trả lời khách quan và sâu sắc nhất.
- Hỗ trợ thiết lập `MinSuccessfulExperts` để đảm bảo tính chịu lỗi nếu có expert bị timeout.

### 7. API Keys nội bộ & Quản lý Hạn mức (Quota)
- Enforce bảo mật gateway thông qua `REQUIRE_API_KEY=true`.
- Quản lý hạn mức chi tiết cho từng API Key cục bộ:
  - Giới hạn danh sách model được phép gọi.
  - Giới hạn số lượng request tối đa.
  - Giới hạn số token tiêu thụ (input + output).
  - Giới hạn tổng chi phí sử dụng bằng USD.
  - Tự động thống kê lượng tiêu thụ thực tế theo thời gian thực.

### 8. Thống kê Usage, Pricing & Ngân sách (Budget)
- **Dashboard Thống kê & Biểu đồ**:
  - Biểu đồ thời gian thực trực quan hóa token và chi phí.
  - Bảng thống kê chi tiết theo Model, Provider/Account, API Key nội bộ, và Endpoint.
  - Hỗ trợ gom nhóm dữ liệu mở rộng (expandable parent/child rows) giúp xem nhanh chi tiết phân bổ.
  - Đồng bộ hóa toàn bộ biểu đồ và bảng dữ liệu theo các mốc thời gian: **Today, 24h, 7D, 30D**.
  - Metric tự động định dạng rút gọn (K, M, B) và hiển thị dấu phẩy dễ đọc.
- **Custom Pricing**: Cho phép cấu hình giá token chi tiết cho từng Model/Provider (Input, Output, Cached Input, Reasoning). Hỗ trợ override Context Length, RPM, TPM và hạn mức ngày.
- **Daily/Monthly Budget**: Thiết lập hạn mức chi tiêu hàng ngày/hàng tháng cho toàn gateway, tự động phát cảnh báo hoặc ngắt kết nối khi chi phí vượt ngưỡng (ví dụ: 80% ngân sách).

### 9. Admin Security & SSRF Mitigation
- **Admin Passcode**: Bảo vệ toàn bộ trang dashboard quản trị và các API `/api/*` bằng mật khẩu khi bật chế độ bảo mật admin.
- **Secret Masking**: Tự động ẩn các API Key, Passcode và Token (`********...`) trong tất cả các payload JSON trả về giao diện.
- **SSRF Mitigation**: Chặn đứng nguy cơ tấn công SSRF từ các endpoint kiểm tra proxy bằng cách cấm kết nối tới toàn bộ dải IP local/private/loopback (như `127.0.0.1`, `192.168.*`, `10.*`, metadata IP `169.254.169.254`).

### 10. Localization (Đa ngôn ngữ)
- Hỗ trợ đầy đủ tiếng Việt (`vi`) và tiếng Anh (`en`).
- Tự động nhận diện ngôn ngữ ưu tiên thông qua cookie `vivurouter_lang` hoặc query parameter `?lang=`.

---

## 🛠 Yêu cầu hệ thống

- **Go version**: 1.25+
- Không cần cài đặt Node.js hay bất kỳ dependency runtime cồng kềnh nào khác. Hệ thống tự động đóng gói sẵn toàn bộ static assets và templates trong binary khi build.

---

## ⚡ Khởi chạy nhanh

1. Clone dự án và truy cập thư mục ứng dụng:
   ```bash
   cd vivurouter-go
   ```

2. Chạy ứng dụng trực tiếp:
   ```bash
   go run ./cmd/vivurouter-go
   ```

3. Truy cập vào dashboard quản trị tại địa chỉ mặc định:
   ```text
   http://127.0.0.1:20129
   ```

---

## ⚙️ Cấu hình môi trường

Hệ thống hỗ trợ cấu hình nhanh thông qua file `.env`. Bạn có thể sao chép file cấu hình mẫu:
```bash
cp .env.example .env
```

Các biến môi trường quan trọng:

| Biến môi trường | Giá trị mặc định | Mô tả |
|---|---|---|
| `HOSTNAME` | `127.0.0.1` | Địa chỉ IP bind server. Nên giữ `127.0.0.1` khi chạy cá nhân. |
| `PORT` | `20129` | Port chạy cổng gateway và dashboard. |
| `STORE_BACKEND` | `file` | Cơ chế lưu trữ: `file` (JSON) hoặc `sqlite` (SQLite không cần CGO). |
| `DATA_DIR` | `./data` | Thư mục lưu trữ database, logs và các file debug payload. |
| `REQUIRE_API_KEY` | `false` | Bắt buộc client gọi gateway phải gửi kèm header `Authorization` / `x-api-key`. |
| `LOCAL_API_KEY` | `sk-local-demo` | API key local mặc định nếu danh sách key trống. |
| `DEBUG` | `false` | Bật chế độ debug (tự động mở thêm các endpoint `/debug/pprof/` trên IP loopback). |

*Lưu ý:* Khi chạy lần đầu, nếu chưa có file database trong `DATA_DIR`, hệ thống sẽ tự động seed dữ liệu mẫu dựa trên các biến môi trường cấu hình trong `.env` như `OPENAI_API_KEY`, `CODEX_ACCESS_TOKEN`,... Các lần chạy sau hệ thống sẽ ưu tiên đọc dữ liệu đã lưu.

---

## 📁 Cấu trúc thư mục dự án

```text
vivurouter-go/
├── cmd/
│   └── vivurouter-go/         # Entrypoint của ứng dụng (main.go)
├── internal/
│   ├── app/                   # Đăng ký routes, khởi tạo server và middleware
│   ├── auth/                  # Xác thực gateway API key và quản lý quota
│   ├── config/                # Đọc cấu hình từ biến môi trường
│   ├── dashboard/             # Xử lý giao diện web admin, backup/restore, i18n
│   ├── gateway/               # Xử lý API gateway, combos, fusion, prompt router, pricing
│   ├── observe/               # Ghi nhận log request, đo lường metrics và quản lý cooldown
│   ├── provider/              # Các adapter kết nối upstream (OpenAI, Codex, Mimo, Antigravity)
│   ├── store/                 # Lớp dữ liệu trừu tượng (JSON File Store và SQLite Store)
│   ├── tokenopt/              # Tối ưu hóa / nén token đầu vào trước khi gửi đi
│   ├── translator/            # Bộ chuyển đổi định dạng request Anthropic <-> OpenAI/Codex
│   └── rtkbridge/             # Tích hợp phát hiện Runtime Key
├── web/
│   ├── static/                # Static assets (CSS, JS) phục vụ giao diện dashboard
│   └── templates/             # Các Go HTML templates kết xuất giao diện
├── client/                    # Thư mục chứa cấu hình build app client đóng gói
├── scripts/                   # Các script tự động hóa (smoke check, build, package)
├── CHANGELOG.md               # Nhật ký các phiên bản cập nhật
└── AI.md                      # Tài liệu kỹ thuật chi tiết dành cho nhà phát triển / AI
```

---

## 🛠 Kiểm tra & Đóng gói (Build & Test)

### 1. Định dạng code & chạy kiểm thử
```powershell
gofmt -w ./cmd ./internal
go test ./...
```

### 2. Biên dịch binary
```powershell
go build ./cmd/vivurouter-go
```

### 3. Chạy script kiểm thử API (Smoke Test)
Để đảm bảo cổng API gateway hoạt động chuẩn xác, hãy chạy script smoke check tích hợp:
```powershell
.\scripts\smoke.ps1
```

### 4. Đóng gói phân phối đa nền tảng
Chúng tôi cung cấp script tự động biên dịch và gom asset phục vụ cho việc tạo release:
- Trên Windows:
  ```powershell
  .\scripts\package-release.ps1
  ```
- Trên Linux/MacOS:
  ```bash
  ./scripts/package-release.sh
  ```
Kết quả đóng gói sẽ nằm trong thư mục `dist/` dưới dạng các file nén `.zip` hoặc `.tar.gz` chứa file thực thi kèm assets cần thiết.

---

## 🤝 Tích hợp Client (Hướng dẫn)

### Tích hợp với Claude Code / Claude CLI
Để sử dụng VivuRouter làm cổng chuyển đổi cho Claude Code sử dụng OpenAI-compatible hoặc Codex:
1. Đảm bảo VivuRouter đang chạy ở cổng `20129`.
2. Mở terminal và thiết lập biến môi trường trước khi chạy Claude Code:
   ```bash
   # Windows PowerShell
   $env:ANTHROPIC_BASE_URL="http://127.0.0.1:20129/v1"
   claude
   
   # Linux/MacOS Bash
   export ANTHROPIC_BASE_URL="http://127.0.0.1:20129/v1"
   claude
   ```
3. Nếu bạn bật `REQUIRE_API_KEY=true` trên VivuRouter, hãy gửi kèm API key của bạn:
   ```bash
   export ANTHROPIC_API_KEY="sk-local-xxxx"
   ```

### Tích hợp với RooCode (VSCode Extension)
1. Chọn **Provider**: `OpenAI Compatible` hoặc `OpenRouter` hoặc `Custom`.
2. Điền **Base URL**: `http://127.0.0.1:20129/v1`.
3. Điền **API Key**: API key nội bộ bạn đã cấu hình tại trang `/api-keys` (hoặc mặc định `sk-local-demo` nếu chưa bật).
4. Điền **Model ID**: Nhập chính xác tên model thực tế hoặc tên combo model ảo bạn đã cấu hình. VivuRouter sẽ tự động trả về thông tin context window đã được cấu hình tại trang `/pricing` hoặc mặc định của hệ thống để RooCode hiển thị chuẩn xác bối cảnh làm việc.

---

## 📝 Giới hạn & Điểm lưu ý hiện tại
- **Cooldown**: Cơ chế cooldown các provider khi gặp lỗi 429 hoặc lỗi mạng hiện tại được quản lý trong bộ nhớ (In-memory), sẽ bị xóa nếu khởi động lại server.
- **Cache behavior của Codex**: Trình dịch Codex sử dụng prompt cache từ upstream. Mức cache cố định quan sát được thường là phần static prefix của Claude Code và tool schema, phần nội dung hội thoại phía sau có thể không được cache toàn bộ. Nếu gặp lỗi timeout đối với context cực lớn (>150k tokens), hãy xem xét thu gọn lịch sử trò chuyện.
- **Diagnostic Log**: Bật chế độ `Save Raw Prompt` trong phần settings sẽ ghi nhận toàn bộ payload vào thư mục `data/debug-payloads/`. Hãy cân nhắc tắt tính năng này khi vận hành thực tế để tiết kiệm dung lượng ổ đĩa.

## Cảm ơn
Được lấy cảm hứng từ 9router  

## Dự án phục vụ mục đích cá nhân các bản cập nhật đều dựa trên nhu cầu cá nhân nếu bạn cần muốn theo ý mình vui lòng tạo 1 Fork hoặc chờ :))
