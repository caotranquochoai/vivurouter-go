# Tài liệu kiểm thử giao diện và tính năng VivuRouter Go

## 1. Mục tiêu kiểm thử

Tài liệu này dùng để hướng dẫn nhân viên kiểm thử toàn bộ các tính năng và giao diện mới của trang web VivuRouter Go, tập trung vào:

- Dashboard usage/cost/request logs.
- Quản lý Providers.
- OAuth Codex.
- Fetch models, quota, test model.
- Proxy theo Provider.
- Quản lý API key nội bộ.
- Custom model pricing và rate limit theo Provider/Model.
- Tính đúng token, request và chi phí.
- Tính dễ dùng, lỗi giao diện và các điểm cần cải thiện.

Nhân viên kiểm thử cần phản hồi đầy đủ lỗi, hành vi không đúng, đề xuất thiết lập mặc định, đề xuất cải thiện giao diện, text hướng dẫn chưa rõ, luồng thao tác khó hiểu và các tình huống dễ gây nhầm lẫn.

---

## 2. Chuẩn bị môi trường

### 2.1. Chạy ứng dụng

Trong thư mục `vivurouter-go`, chạy:

```bash
go run ./cmd/vivurouter-go
```

Mặc định web dashboard chạy tại:

```text
http://localhost:20129/dashboard
```

Endpoint OpenAI-compatible mặc định:

```text
http://localhost:20129/v1
```

### 2.2. Dữ liệu cần chuẩn bị

Nhân viên kiểm thử nên chuẩn bị tối thiểu:

- 01 Provider OpenAI-compatible có API key hợp lệ.
- 01 Provider Codex OAuth hợp lệ nếu có tài khoản Codex.
- 01 proxy HTTP/SOCKS nếu cần kiểm thử proxy.
- Một vài model test, ví dụ:
  - `gpt-4.1`
  - `gpt-4.1-mini`
  - `gpt-4o-mini`
  - `cx/gpt-5.5`

---

## 3. Quy tắc phản hồi bắt buộc

Khi phát hiện lỗi hoặc đề xuất cải thiện, nhân viên kiểm thử cần ghi theo mẫu sau:

```md
### Mã lỗi / góp ý
- Trang: Dashboard / Providers / Settings / Requests / API
- Tính năng: tên tính năng
- Mức độ: Critical / High / Medium / Low / UI Suggestion
- Thiết bị / trình duyệt: Chrome, Edge, Firefox, mobile, desktop
- Các bước tái hiện:
  1. ...
  2. ...
  3. ...
- Kết quả thực tế:
- Kết quả mong muốn:
- Ảnh chụp / video / log console:
- Gợi ý sửa:
```

Nếu là góp ý giao diện, cần trả lời thêm:

```md
- Phần nào khó hiểu?
- Text nào cần đổi?
- Nút nào nên đổi vị trí/màu/kích thước?
- Có cần tooltip/hướng dẫn nhanh không?
- Có bị tràn, vỡ layout, khó bấm trên mobile không?
```

Nếu là góp ý thiết lập mặc định, cần trả lời thêm:

```md
- Giá trị mặc định hiện tại:
- Giá trị đề xuất:
- Lý do đề xuất:
- Có ảnh hưởng tới người dùng mới không?
```

---

## 4. Kiểm thử Dashboard

URL:

```text
/dashboard
```

### 4.1. KPI cards

Kiểm tra các ô:

- Total requests.
- Total tokens.
- Cost.
- Providers.

Cần xác nhận:

- Tổng request tăng sau khi gọi API thật.
- Tổng request tăng sau khi bấm test model trên UI.
- Total tokens tăng hợp lý.
- Cost không luôn bằng 0 nếu model có pricing.
- Provider enabled/total đúng.
- Cooldown count đúng nếu provider bị lỗi upstream.

Cần phản hồi nếu:

- Token sai lệch bất thường.
- Cost không tính dù đã cấu hình giá.
- Request test không được cộng.
- Card bị tràn hoặc khó đọc.

### 4.2. Token & Cost Analytics

Kiểm tra các trường:

- Input.
- Output.
- Cached.
- Reasoning.

Cần xác nhận:

- Nếu upstream trả usage thật thì dashboard ưu tiên usage thật.
- Nếu upstream không trả usage thì dashboard dùng ước lượng.
- Estimated logs hiển thị hợp lý.

Ghi chú lỗi nếu:

- Input/output bị đảo.
- Total tokens không bằng tổng hợp lý.
- Reasoning/cached không hiển thị khi response có dữ liệu.

### 4.3. Request gần đây

Kiểm tra phần `Request gần đây`.

Yêu cầu hiện tại:

- Chỉ hiển thị model.
- Thời gian dạng ngắn:
  - `12s`: cách đây 12 giây.
  - `5p`: cách đây 5 phút.
  - `2h`: cách đây 2 giờ.
  - `1d`: cách đây 1 ngày.
- Tổng token.
- Không tràn layout.

Cần test trên:

- Desktop full width.
- Màn hình hẹp.
- Mobile viewport.
- Model ID rất dài.
- Token rất lớn.

Cần phản hồi nếu:

- Dòng request bị tràn ngang.
- Model dài làm vỡ layout.
- Time format khó hiểu.
- Cần tooltip hiển thị thời gian đầy đủ.

---

## 5. Kiểm thử trang Providers

URL:

```text
/providers
```

### 5.1. Nhóm Provider

Kiểm tra hai nhóm chính:

- OAuth Providers.
- API Key Providers.

Cần xác nhận:

- Nút `+` trong từng nhóm hoạt động.
- Popup nhập thông tin đúng loại provider.
- Đóng popup bằng nút `×`, nút hủy và phím Escape.
- Form không bị mất dữ liệu bất ngờ nếu thao tác sai.

### 5.2. Thêm API Key Provider

Kiểm tra các trường:

- ID.
- Name.
- Type.
- Base URL.
- API Key.
- Proxy URL.
- Models.

Cần xác nhận:

- Lưu provider thành công.
- Provider mới xuất hiện đúng nhóm.
- Có thể enable/disable.
- Có thể xóa.
- Secret/API key không lộ toàn bộ nếu UI có mask.

### 5.3. Thêm Codex OAuth Provider

Kiểm tra:

- Bấm `+` trong OAuth Providers.
- Tạo link authorize.
- Link dùng đúng OpenAI auth.
- Callback local hoạt động.
- Sau khi cấp quyền, provider có token và hiển thị connected.

Cần phản hồi nếu:

- Link authorize không mở.
- Callback lỗi.
- Không lưu token.
- UI không báo trạng thái rõ.

### 5.4. Proxy theo Provider

Kiểm tra:

- Thêm proxy khi tạo provider.
- Sửa proxy provider đã có.
- Fetch models qua proxy.
- Test model qua proxy.
- Codex OAuth/token refresh qua proxy nếu có.

Cần phản hồi nếu:

- Proxy không được dùng từ request đầu tiên.
- Không có thông báo khi proxy sai.
- UI chưa cảnh báo định dạng proxy hợp lệ.

---

## 6. Kiểm thử Fetch models, Quota, Test model

### 6.1. Lấy models từ API

Trên provider card, bấm:

```text
Lấy models từ API
```

Cần xác nhận:

- Button chuyển trạng thái loading.
- Danh sách model cập nhật.
- Models được lưu lại sau reload.
- Model chip có nút test.

Cần phản hồi nếu:

- Bấm không có gì xảy ra.
- Không báo lỗi khi API fail.
- Model trùng lặp.
- Model list quá dài gây lag hoặc tràn.

### 6.2. Kiểm tra quota Codex

Với provider Codex, bấm:

```text
Quota
```

Cần xác nhận:

- Hiển thị plan.
- Hiển thị quota windows nếu API trả dữ liệu.
- Có progress bar.
- Báo limit reached rõ ràng nếu bị giới hạn.

Cần phản hồi nếu:

- Không phân biệt quota thường và review quota.
- Reset time khó hiểu.
- Thiếu thông tin quan trọng từ upstream.

### 6.3. Test model

Bấm `Test` trên từng model.

Cần xác nhận:

- Hiển thị OK/fail.
- Latency hiển thị đúng.
- Request test được ghi vào Requests log.
- Request test được tính vào tổng request dashboard.
- Token và cost của test request được tính hợp lý.

Cần phản hồi nếu:

- Test thành công nhưng log không có.
- Token luôn là 2 hoặc quá thấp bất thường.
- Cost luôn bằng 0 dù có pricing.
- Lỗi upstream không rõ nguyên nhân.

---

## 7. Kiểm thử Settings & API Keys

URL:

```text
/settings
```

### 7.1. Gateway settings

Kiểm tra:

- Default provider.
- Default Codex provider.
- Keep request logs.
- Local API key.
- Dashboard message.
- Require API key.

Cần xác nhận:

- Lưu settings thành công.
- Reload vẫn giữ settings.
- Require API key bật/tắt đúng hành vi với `/v1/*`.

### 7.2. Tạo API key mới

Bấm nút `+` trong khu vực API Keys.

Kiểm tra form:

- Name / ID.
- Secret key.
- Chọn provider.
- Chọn model.
- Allowed models.
- Max requests.
- Max tokens.
- Max USD.

Yêu cầu:

- Có thể chọn provider/model từ danh sách có sẵn.
- Model được chọn tự thêm vào Allowed models.
- Có thể nhập thủ công Allowed models.
- Có thể nhập `*` để cho phép toàn bộ model.
- Có thể nhập nhiều model phân tách bằng dấu phẩy.
- Nếu bỏ trống key, hệ thống tự sinh key.

Cần test:

- Key chỉ được dùng model được phép.
- Key bị chặn khi vượt request quota.
- Key bị chặn khi vượt token quota.
- Key bị chặn khi vượt USD quota.
- Xóa API key card rồi lưu có xóa thật không.
- Tạo nhiều key có bị trùng ID/key không.

Cần phản hồi nếu:

- Picker provider/model khó hiểu.
- Không biết model đã được thêm vào Allowed models.
- Cần tag/chip thay vì text input.
- Cần nút copy API key.
- Cần nút regenerate key.
- Cần trạng thái enabled/disabled từng key.

---

## 8. Kiểm thử Custom model pricing và rate limit

### 8.1. Chọn model có sẵn

Trong Custom model pricing:

- Chọn provider có models.
- Bấm `Thiết lập` trên model chip.
- Popup mở ra với provider/model đã điền sẵn.

Cấu hình cần test:

- Input USD / 1M.
- Output USD / 1M.
- Cached input USD / 1M.
- Reasoning USD / 1M.
- RPM.
- TPM.
- Daily requests.
- Daily tokens.

Cần xác nhận:

- Lưu thiết lập tạo pricing card.
- Cấu hình vẫn còn sau reload.
- Có thể xóa pricing card.
- Nếu thiết lập lại cùng provider/model thì cập nhật/thay thế rule cũ.
- Cost dashboard dùng giá custom mới.

### 8.2. Nhập thủ công provider/model

Trong popup pricing:

- Nhập provider thủ công.
- Nhập model thủ công.
- Lưu.

Cần xác nhận:

- Model chưa có trong provider list vẫn lưu được.
- Giá custom áp dụng khi request dùng model đó.

### 8.3. Rate limit theo model

Hiện tại UI cho phép nhập rate limit theo model. Nhân viên kiểm thử cần đánh giá:

- Field rate limit đã đủ chưa?
- Nên thêm giới hạn theo giờ/tháng không?
- Cách đặt tên RPM/TPM có dễ hiểu không?
- Có cần tooltip giải thích không?
- Có cần hiển thị rate limit ở Provider card hoặc Dashboard không?

Cần ghi rõ nếu phát hiện rate limit chỉ lưu cấu hình nhưng chưa enforce ở gateway, hoặc cần bổ sung enforcement.

---

## 9. Kiểm thử Requests page

URL:

```text
/requests
```

Cần xác nhận:

- Trang vẫn hiển thị bảng log chi tiết.
- Log có endpoint, provider, model, status, duration, token, cost, error.
- Test request cũng xuất hiện.
- Request thật qua `/v1/chat/completions`, `/v1/responses`, `/codex/responses` xuất hiện.

Cần phản hồi nếu:

- Bảng quá rộng.
- Cost format khó đọc.
- Error quá dài làm vỡ layout.
- Cần filter theo provider/model/status/API key.

---

## 10. Kiểm thử API thực tế

### 10.1. List models

```bash
curl http://localhost:20129/v1/models
```

Cần xác nhận:

- Trả danh sách model hợp lệ.
- Model ID đúng format.
- Không lộ secret.

### 10.2. Chat completions

```bash
curl http://localhost:20129/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_LOCAL_KEY" \
  -d '{"model":"gpt-4.1-mini","messages":[{"role":"user","content":"hi"}],"max_tokens":32}'
```

Cần xác nhận:

- Response đúng OpenAI-compatible.
- Request log có token/cost.
- API key quota tăng.

### 10.3. Responses/Codex

Kiểm tra nếu có Codex provider:

```bash
curl http://localhost:20129/v1/responses \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_LOCAL_KEY" \
  -d '{"model":"cx/gpt-5.5","input":"hi"}'
```

Cần xác nhận:

- Route đúng provider Codex.
- Log token/cost hợp lý.
- Lỗi OAuth/quota hiển thị rõ.

---

## 11. Kiểm thử responsive và trình duyệt

Cần kiểm tra trên:

- Chrome desktop.
- Edge desktop.
- Firefox nếu có.
- Mobile width 390px.
- Tablet width 768px.

Các điểm cần chú ý:

- Modal có bị vượt chiều cao màn hình không.
- Button có đủ lớn để bấm trên mobile không.
- Table/log có tràn ngang không.
- Provider/model chip list quá dài có scroll hợp lý không.
- Sticky save bar có che nội dung không.
- Dark theme có tương phản đủ không.

---

## 12. Checklist tổng hợp cho QA

### Dashboard

- [ ] KPI request/token/cost đúng.
- [ ] Test request được tính vào tổng request.
- [ ] Recent requests không tràn.
- [ ] Time hiển thị dạng `s/p/h/d`.
- [ ] Cost thay đổi khi có custom pricing.

### Providers

- [ ] Thêm OAuth provider được.
- [ ] Thêm API key provider được.
- [ ] Fetch models hoạt động.
- [ ] Quota Codex hoạt động.
- [ ] Test model hoạt động.
- [ ] Proxy hoạt động và báo lỗi rõ nếu sai.

### Settings / API Keys

- [ ] Tạo API key bằng modal được.
- [ ] Chọn provider/model cho Allowed models được.
- [ ] Nhập thủ công Allowed models được.
- [ ] Quota request/token/USD hoạt động.
- [ ] Xóa key rồi lưu hoạt động.

### Custom pricing

- [ ] Chọn model từ Provider list được.
- [ ] Popup pricing tự điền provider/model.
- [ ] Lưu giá và rate limit được.
- [ ] Xóa pricing rule được.
- [ ] Cost dùng giá custom.
- [ ] Dữ liệu vẫn còn sau reload.

### Requests

- [ ] Log request thật có đủ thông tin.
- [ ] Log test request có đủ thông tin.
- [ ] Error log không phá layout.
- [ ] Cost/token dễ đọc.

### UI/UX

- [ ] Modal dễ hiểu.
- [ ] Label/placeholder rõ ràng.
- [ ] Có đủ trạng thái loading/success/error.
- [ ] Mobile không vỡ layout.
- [ ] Cần tooltip ở đâu thì ghi rõ.

---

## 13. Phần phản hồi cuối cùng của nhân viên kiểm thử

Sau khi kiểm thử, nhân viên cần gửi báo cáo gồm:

```md
# Báo cáo kiểm thử VivuRouter Go

## 1. Tổng quan
- Ngày test:
- Người test:
- Phiên bản / commit nếu có:
- Trình duyệt / thiết bị:

## 2. Kết quả tổng quan
- Tổng số case đã test:
- Pass:
- Fail:
- Blocked:
- Các tính năng rủi ro cao:

## 3. Danh sách lỗi
### BUG-001
- Trang:
- Mức độ:
- Bước tái hiện:
- Kết quả thực tế:
- Kết quả mong muốn:
- Screenshot/log:
- Gợi ý sửa:

## 4. Góp ý giao diện
- Khu vực cần sửa:
- Vấn đề:
- Đề xuất cụ thể:
- Mức ưu tiên:

## 5. Góp ý thiết lập mặc định
- Setting hiện tại:
- Setting đề xuất:
- Lý do:

## 6. Góp ý tính năng nên bổ sung
- Tính năng:
- Lý do:
- Mức ưu tiên:

## 7. Kết luận
- Có thể release chưa: Có / Không
- Điều kiện cần sửa trước khi release:
```

Yêu cầu phản hồi phải đầy đủ, có bước tái hiện rõ ràng, kèm ảnh/log nếu có, và luôn ghi đề xuất sửa cụ thể cho từng lỗi hoặc điểm chưa tốt.
