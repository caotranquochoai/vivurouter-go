# Yêu cầu QA kiểm tra lại — Round 2

Tài liệu này tổng hợp các hạng mục đã xử lý sau phản hồi retest ngày 08/06/2026. QA vui lòng kiểm tra lại từng mục theo danh sách dưới đây và phản hồi `Pass / Fail / Partial` ,log nếu còn lỗi.

## 1. Phạm vi retest

Vui lòng kiểm tra lại các trang/chức năng sau:

- Dashboard: `http://127.0.0.1:20129/dashboard`
- Providers: `http://127.0.0.1:20129/providers`
- Settings: `http://127.0.0.1:20129/settings`
- Requests: `http://127.0.0.1:20129/requests`
- API test model: `/api/providers/test-model`
- Gateway endpoints: `/v1/chat/completions`, `/v1/responses`, `/codex/responses`

## 2. Danh sách hạng mục đã xử lý cần QA kiểm tra lại

### R2-FIX-001 — Dashboard Cost phân biệt rõ trường hợp thiếu pricing rule

Liên quan QA report:

- BUG-002 còn sót: Cost vẫn `$0.000000` dù request có token thật.

Đã xử lý:

- Dashboard Cost hiện có note/tooltip để phân biệt:
  - chưa có request/token để tính cost;
  - có token nhưng chưa có pricing rule;
  - đã có custom pricing/pricing rule.
- Nếu request có token nhưng không match pricing rule, cost vẫn có thể là `$0`, nhưng UI sẽ hiển thị rõ dạng `request có token nhưng chưa có pricing rule`, không im lặng như trước.

QA cần kiểm tra:

1. Vào Settings → Custom model pricing.
2. Đảm bảo chưa tạo pricing rule cho một provider/model đang test, ví dụ `vivurouter/GPT`.
3. Gửi request để tạo log có token > 0.
4. Vào Dashboard, xem card Cost.
5. Kiểm tra phần note/tooltip dưới card Cost.
6. Tạo pricing rule đúng provider/model, gửi request mới, kiểm tra cost bắt đầu tăng.

Kỳ vọng:

- Khi thiếu pricing rule nhưng có token, UI phải báo rõ `chưa có pricing rule` hoặc tương đương.
- Khi có pricing rule đúng, Cost phải tính theo token và giá đã cấu hình.

### R2-FIX-002 — Copy Local API key có feedback visual và xử lý lỗi clipboard

Liên quan QA report:

- FIX-007 partial: Nút Copy có nhưng thiếu feedback visual.

Đã xử lý:

- Sau khi copy thành công, nút đổi thành `Copied ✓` trong 2 giây.
- Nếu Clipboard API bị chặn/từ chối, nút hiển thị `Không thể copy`, input được focus và select để người dùng copy thủ công.

QA cần kiểm tra:

1. Vào Settings.
2. Nhập hoặc có sẵn Local API key.
3. Nhấn Copy.
4. Kiểm tra nút đổi sang `Copied ✓` rồi quay lại `Copy`.
5. Dán thử ra nơi khác để xác nhận clipboard.
6. Có thể test thêm trong môi trường browser chặn clipboard nếu có.

Kỳ vọng:

- Người dùng thấy feedback rõ ràng sau khi nhấn Copy.
- Nếu copy thất bại, UI không im lặng.

### R2-FIX-003 — Credentials Codex có tooltip an toàn, dễ hiểu

Liên quan QA report:

- FIX-006 partial: Credentials `access...` thiếu tooltip.

Đã xử lý:

- Element Credentials có `title` tooltip.
- Nội dung tooltip không lộ secret thật, ví dụ:
  - `access_token (OAuth, đã lưu)`
  - `refresh_token (đã lưu)`
  - `api_key (đã lưu)`

QA cần kiểm tra:

1. Vào Providers.
2. Tìm Codex provider đã OAuth hoặc có access token.
3. Hover vào field Credentials.
4. Kiểm tra tooltip hiển thị mô tả token đã lưu.

Kỳ vọng:

- Tooltip xuất hiện khi hover.
- Tooltip không chứa token thật.
- Người dùng hiểu provider đang có OAuth token/credential hợp lệ.

### R2-FIX-004 — Test model có timeout, elapsed counter và Cancel

Liên quan QA report:

- BUG-006: Test model mất 18–19 giây, không timeout, không Cancel, không elapsed time.

Đã xử lý backend:

- `/api/providers/test-model` dùng timeout mặc định 15 giây.
- Khi timeout, API trả lỗi rõ: `upstream timeout after 15s`.
- Request timeout vẫn được ghi log với token ước lượng.

Đã xử lý frontend:

- Khi test model, UI hiển thị elapsed counter dạng `Testing... 0s`, `Testing... 3s`, `Testing... 7s`.
- Nút `Cancel` xuất hiện sau 3 giây.
- Nhấn Cancel sẽ hủy request bằng AbortController.
- Sau timeout/cancel, chip chuyển sang `Timeout`/lỗi timeout hoặc `Cancelled`, không treo mãi.

QA cần kiểm tra:

1. Vào Providers.
2. Chọn một model test bình thường, xác nhận vẫn OK.
3. Chọn provider/model phản hồi chậm hoặc giả lập mạng chậm.
4. Quan sát elapsed counter.
5. Chờ quá 3 giây, kiểm tra nút Cancel xuất hiện.
6. Nhấn Cancel và kiểm tra UI hiển thị `Cancelled`.
7. Không cancel, chờ timeout khoảng 15 giây và kiểm tra lỗi timeout.
8. Vào Requests, kiểm tra log `/api/providers/test-model` được ghi lại.

Kỳ vọng:

- UI không treo im lặng.
- Timeout rõ ràng sau khoảng 15 giây.
- Cancel hoạt động và reset nút Test về trạng thái ban đầu.

### R2-FIX-005 — Badge Default phân biệt OpenAI và Codex

Liên quan QA report:

- NEW-001: Hai provider cùng badge `Default` gây nhầm lẫn.

Đã xử lý:

- Provider openai-compatible mặc định hiển thị `Default OpenAI`.
- Provider Codex mặc định hiển thị `Default Codex`.
- Badge có tooltip giải thích:
  - `Provider mặc định cho /v1/chat/completions`.
  - `Provider mặc định cho /v1/responses và /codex/responses`.

QA cần kiểm tra:

1. Vào Settings → Gateway.
2. Chọn Default provider và Default Codex provider khác nhau.
3. Vào Providers.
4. Kiểm tra badge trên từng provider.
5. Hover badge để xem tooltip.

Kỳ vọng:

- Không còn hai badge `Default` chung chung.
- Người dùng hiểu provider nào dùng cho endpoint nào.

### R2-FIX-006 — Tooltip timestamp cột Time dùng local time

Liên quan QA report:

- FIX-003 cần xác nhận tooltip full timestamp.

Đã xử lý:

- Element thời gian trong Requests có `data-timestamp`.
- JavaScript chuyển tooltip sang local time của browser bằng `toLocaleString()`.

QA cần kiểm tra:

1. Vào Requests.
2. Cột Time vẫn hiển thị relative time như `22s`, `1h`, `2d`.
3. Hover vào thời gian.
4. Kiểm tra tooltip full timestamp hiển thị theo local time, không phải UTC raw khó đọc.

Kỳ vọng:

- Relative time vẫn gọn.
- Tooltip hiển thị full time theo local timezone của máy test.

### R2-FIX-007 — Badge row Providers không còn rơi hàng lộn xộn

Liên quan QA report:

- FIX-006 partial: `Direct IP` bị xuống hàng riêng nhìn như bug layout.

Đã xử lý:

- Badge row dùng flex-wrap và gap thống nhất.
- Thứ tự badge hiện là status → auth → proxy → default.

QA cần kiểm tra:

1. Vào Providers.
2. Kiểm tra Codex card có nhiều badge như Error/OAuth/Direct IP/Default Codex.
3. Resize browser hẹp/rộng.

Kỳ vọng:

- Badge có thể wrap khi thiếu chiều rộng nhưng spacing đồng đều.
- Không còn cảm giác `Direct IP` là lỗi layout riêng biệt.

### R2-FIX-008 — Keep request logs mặc định và tài liệu store cũ

Liên quan QA report:

- FIX-012 chưa verify trên store cũ.

Đã xử lý/document:

- Giá trị mặc định mới vẫn là `1000` cho store/settings mới.
- Đã ghi rõ trong changelog: store cũ có thể giữ giá trị đã lưu trước đó, người dùng có thể chỉnh trực tiếp trong Settings.

QA cần kiểm tra:

1. Với store mới/reset data, kiểm tra `Keep request logs` mặc định là `1000`.
2. Với store cũ, kiểm tra vẫn có thể chỉnh thủ công từ `200` lên `1000` và lưu thành công.
3. Đọc changelog QA fixes để xác nhận nội dung đã document.

Kỳ vọng:

- Store mới mặc định `1000`.
- Store cũ không bị hiểu nhầm là lỗi nếu vẫn giữ giá trị user/settings cũ.

### R2-FIX-009 — API key có Enable/Disable toggle

Liên quan QA report:

- UX: Không có feedback trạng thái enabled/disabled cho từng API key.

Đã xử lý:

- API key card có nút `Enable/Disable`.
- Badge chuyển giữa `Active` và `Disabled`.
- Trạng thái enabled được lưu vào hidden settings text và parse lại khi reload.
- Key disabled vẫn lưu trong settings nhưng gateway không chấp nhận key đó.

QA cần kiểm tra:

1. Vào Settings → API Keys.
2. Tạo một API key policy.
3. Disable key, lưu settings, reload trang.
4. Kiểm tra badge vẫn là Disabled.
5. Bật Require API key.
6. Gửi request bằng key disabled.
7. Enable lại key, lưu và test lại.

Kỳ vọng:

- Disabled key không dùng được.
- Enabled key dùng lại được nếu quota/model scope hợp lệ.
- Trạng thái không mất sau reload.

### R2-FIX-010 — Local API key có Regenerate

Liên quan QA report:

- UX: Không có nút Regenerate cho Local API key.

Đã xử lý:

- Thêm nút `Regenerate` cạnh nút Copy.
- Khi bấm, hiện confirm: `Regenerate key sẽ làm mất key cũ. Tiếp tục?`.
- Nếu xác nhận, sinh key mới dạng `sk-local-...`.
- Người dùng cần bấm `Lưu settings` để lưu key mới.

QA cần kiểm tra:

1. Vào Settings.
2. Nhấn Regenerate.
3. Chọn Cancel, kiểm tra key không đổi.
4. Nhấn lại Regenerate, chọn OK, kiểm tra key đổi.
5. Lưu settings, reload trang, kiểm tra key mới còn đó.

Kỳ vọng:

- Có confirm trước khi đổi key.
- Key mới không tự mất sau khi lưu/reload.

### R2-FIX-011 — Cảnh báo khi bật Require API key nhưng chưa có API key policy

Liên quan QA report:

- UX: Bật `Require API key` khi chưa có key có thể chặn toàn bộ endpoint mà không cảnh báo.

Đã xử lý:

- Khi checkbox `Require API key cho /v1/*` được bật và chưa có API key policy nào, UI hiển thị cảnh báo inline.
- Cảnh báo nêu rõ bật tùy chọn này sẽ chặn request đến `/v1/*` nếu chưa tạo API key.

QA cần kiểm tra:

1. Vào Settings với danh sách API Keys rỗng.
2. Bật checkbox Require API key.
3. Kiểm tra cảnh báo inline xuất hiện.
4. Tạo một API key policy.
5. Kiểm tra cảnh báo biến mất hoặc không còn gây nhầm lẫn.

Kỳ vọng:

- Người dùng được cảnh báo trước khi tự khóa endpoint.

## 3. Changelog đi kèm

Đã tạo changelog tại:

```text
vivurouter-go/docs/CHANGELOG_QA_FIXES.md
```

QA vui lòng đọc nhanh changelog để đối chiếu các điểm đã thay đổi.

## 4. Kết quả kiểm tra nội bộ trước khi gửi QA

Đã chạy thành công:

```text
gofmt: pass
go test ./...: pass
go build ./cmd/vivurouter-go: pass
```

## 5. Mẫu phản hồi QA Round 2

Vui lòng phản hồi theo mẫu:

```text
Mã retest: R2-FIX-001 / R2-FIX-002 / ...
Kết quả: Pass / Fail / Partial
Môi trường: Chrome/Edge/Firefox, desktop/mobile, độ phân giải
Dữ liệu test: provider, model, API key, pricing rule nếu liên quan
Các bước đã kiểm tra:
1. ...
2. ...
3. ...
Kết quả thực tế:
Kết quả mong muốn nếu Fail/Partial:
Ảnh chụp/video/log console/network:
Gợi ý bổ sung:
```
