# Yêu cầu QA kiểm tra lại sau khi khắc phục lỗi

Tài liệu này liệt kê các hạng mục đã được khắc phục sau báo cáo kiểm thử thực tế ngày 08/06/2026. QA vui lòng kiểm tra lại từng mục, ghi rõ kết quả Pass/Fail, log console/network nếu còn lỗi.

## 1. Phạm vi kiểm tra lại

Vui lòng kiểm tra lại các trang:

- Dashboard: `http://127.0.0.1:20129/dashboard`
- Providers: `http://127.0.0.1:20129/providers`
- Settings: `http://127.0.0.1:20129/settings`
- Requests: `http://127.0.0.1:20129/requests`
- API gateway: `/v1/chat/completions`, `/v1/responses`, `/codex/responses`

## 2. Các lỗi đã khắc phục và yêu cầu retest

### FIX-001 — Token/cost tracking cho mọi request

Đã khắc phục các lỗi liên quan:

- BUG-001: Total tokens chỉ có 2 dù có nhiều requests.
- BUG-002: Cost luôn `$0.000000` dù đã cài custom pricing.
- BUG-003: Request lỗi hoặc request Codex có `0 tokens`.

Nội dung đã sửa:

- Gateway luôn fallback ước lượng token từ request body khi upstream không trả `usage`.
- Request lỗi mạng, `429`, `5xx`, fallback provider, stream lỗi hoặc response không có `usage` đều vẫn ghi log token ước lượng.
- Cost được tính sau khi đã có token ước lượng, nên custom pricing có thể áp dụng.
- Nếu request body có nội dung nhưng thuật toán ước lượng trả về `0`, hệ thống vẫn ghi tối thiểu `1` input token.

QA cần kiểm tra:

1. Gửi request thành công qua `/v1/chat/completions` hoặc test model trên UI.
2. Gửi request gây lỗi upstream, ví dụ token/quota sai hoặc model lỗi.
3. Vào Dashboard kiểm tra `Total tokens` tăng.
4. Vào Requests kiểm tra request mới có `in`, `out`, `total`; ít nhất `in` phải lớn hơn `0` nếu có payload.
5. Thiết lập custom pricing cho provider/model đang test, gửi request mới, kiểm tra Cost > 0 khi có token.

Kỳ vọng:

- Không còn request mới có `total=0` nếu request body có nội dung.
- Dashboard Cost không còn luôn bằng `$0.000000` khi đã có token và pricing hợp lệ.
- Các request lỗi vẫn có `estimated` token.

### FIX-002 — Dashboard uptime dễ đọc hơn

Đã khắc phục:

- BUG-009: Uptime hiển thị dạng giây thô như `349s`.

Nội dung đã sửa:

- Uptime đổi sang dạng dễ đọc, ví dụ `5m 49s`, `1h 12m`.

QA cần kiểm tra:

1. Vào Dashboard.
2. Xem khu vực `Upstream health`.
3. Kiểm tra dòng Uptime không còn chỉ là số giây thô.

Kỳ vọng:

- Uptime hiển thị dạng thân thiện, dễ đọc.

### FIX-003 — Requests page dùng relative time và tooltip timestamp

Đã khắc phục:

- BUG-004: Trang Requests hiển thị full UTC timestamp quá dài và không nhất quán với Dashboard.

Nội dung đã sửa:

- Cột Time trong Requests đổi sang relative time ngắn như `56p`, `13h`, `2d`.
- Full timestamp được giữ trong tooltip khi hover.

QA cần kiểm tra:

1. Vào `/requests`.
2. Kiểm tra cột Time.
3. Hover vào thời gian để xem tooltip timestamp đầy đủ.

Kỳ vọng:

- Không còn timestamp dài kiểu `2026-06-08 08:09:24.514716 +0000 UTC` hiển thị trực tiếp trong bảng.
- Dashboard và Requests thống nhất cách hiển thị thời gian ngắn.

### FIX-004 — Requests page có thông tin số dòng và filter

Đã khắc phục/cải thiện:

- BUG-007: Không rõ vì sao Requests chỉ hiển thị một phần log.
- UI-005: Không có filter/search trên trang Requests.

Nội dung đã sửa:

- Trang Requests hiển thị rõ `Hiển thị X/Y request gần nhất`.
- Thêm ô filter theo provider, model, status, endpoint, error hoặc nội dung hiển thị trong bảng.

QA cần kiểm tra:

1. Vào `/requests`.
2. Kiểm tra dòng tổng quan `Hiển thị X/Y request gần nhất`.
3. Nhập model/provider/status vào ô filter.
4. Xóa filter và kiểm tra bảng quay lại đầy đủ.

Kỳ vọng:

- QA hiểu rõ đang xem bao nhiêu log trên tổng số log hiện có.
- Filter hoạt động tức thời trên bảng.

### FIX-005 — Provider default lỗi cao có cảnh báo

Đã khắc phục:

- BUG-005: Codex/default provider lỗi 100% nhưng vẫn là Default và không có cảnh báo.

Nội dung đã sửa:

- Nếu provider là Default và error rate > 50%, card provider hiển thị cảnh báo: provider mặc định đang lỗi, cần kiểm tra quota/token hoặc đổi default.

QA cần kiểm tra:

1. Tạo hoặc dùng provider default có nhiều request lỗi.
2. Vào Providers.
3. Kiểm tra card provider có warning rõ ràng.

Kỳ vọng:

- Provider default đang lỗi nhiều phải có cảnh báo nổi bật.

### FIX-006 — Providers badge và credentials dễ hiểu hơn

Đã khắc phục/cải thiện:

- BUG-010: Credentials bị cắt, không có tooltip.
- UI-006: Badge trạng thái quá nhiều và lỗi không nổi bật.
- UI-007: Popup sửa Codex ghi sai nhóm provider.

Nội dung đã sửa:

- Badge status chính được đưa lên trước và lỗi được làm nổi bật hơn.
- Badge `Default` được đặt gần status để dễ nhận biết.
- Credentials có tooltip khi hover, không lộ secret gốc.
- Popup sửa provider Codex hiển thị nhóm OAuth Providers thay vì API Key Providers.

QA cần kiểm tra:

1. Vào Providers.
2. Kiểm tra provider có lỗi, provider default, provider OAuth/Codex.
3. Hover vào credentials.
4. Mở sửa provider Codex.

Kỳ vọng:

- Badge lỗi dễ nhận biết hơn badge phụ.
- Credentials có tooltip mô tả dạng token/secret được cấu hình.
- Popup Codex không còn ghi nhầm `API Key Providers`.

### FIX-007 — Settings checkbox và Local API key UI

Đã khắc phục/cải thiện:

- BUG-008: Label `Require API key cho /v1/*` bị cắt và checkbox tách rời.
- UI-003: Không có nút copy cho Local API key.

Nội dung đã sửa:

- Checkbox và label `Require API key cho /v1/*` hiển thị inline.
- Local API key có nút `Copy`.

QA cần kiểm tra:

1. Vào Settings.
2. Quan sát checkbox `Require API key cho /v1/*`.
3. Nhập Local API key và nhấn Copy.
4. Dán ra nơi khác để kiểm tra nội dung clipboard.

Kỳ vọng:

- Checkbox và label nằm cùng hàng, dễ hiểu.
- Nút Copy hoạt động và đổi nhãn tạm thời thành `Copied`.

### FIX-008 — Pricing card hiển thị đầy đủ trường đã cấu hình

Đã khắc phục:

- UI-004: Pricing card chỉ hiển thị input/output/rpm, thiếu cached/reasoning/TPM/daily limits.

Nội dung đã sửa:

- Pricing card hiển thị thêm:
  - cached input price
  - reasoning price
  - TPM
  - daily requests
  - daily tokens

QA cần kiểm tra:

1. Vào Settings → Custom model pricing.
2. Chọn một model, nhấn thiết lập.
3. Điền input/output/cached/reasoning/RPM/TPM/daily requests/daily tokens.
4. Lưu thiết lập, sau đó lưu settings.
5. Reload trang và kiểm tra card pricing.

Kỳ vọng:

- Card pricing hiển thị đầy đủ các trường đã nhập.
- Dữ liệu không mất sau khi lưu/reload.

### FIX-009 — Model picker pricing bớt khó dùng

Đã cải thiện:

- UI-002: Danh sách model trong Custom pricing bị cắt trong box quá nhỏ.

Nội dung đã sửa:

- Tăng chiều cao vùng danh sách model trong pricing picker.
- Vùng này vẫn có scroll nhưng hiển thị được nhiều model hơn.

QA cần kiểm tra:

1. Vào Settings → Custom model pricing.
2. Kiểm tra provider có nhiều model, ví dụ `vivurouter`.
3. Đánh giá việc cuộn/chọn model có dễ hơn bản trước không.

Kỳ vọng:

- Danh sách model ít bị bó hẹp hơn, thao tác chọn dễ hơn.

### FIX-010 — Codex quota bar thống nhất used/remaining

Đã khắc phục:

- UI-001: Quota bar hiển thị `0%` nhưng text lại `Used 100.0%` gây mâu thuẫn.

Nội dung đã sửa:

- Progress bar hiển thị theo phần đã dùng (`used`).
- Text phụ hiển thị phần còn lại (`Remaining`).

QA cần kiểm tra:

1. Vào Providers.
2. Nhấn Quota cho Codex provider.
3. Kiểm tra các quota card.

Kỳ vọng:

- Nếu Used 100%, bar phải đầy.
- Nếu Remaining 0%, text và bar không mâu thuẫn.

### FIX-011 — Banner lưu cấu hình tự ẩn

Đã khắc phục/cải thiện:

- UI-009: Banner `Đã lưu cấu hình.` không tự biến mất.

Nội dung đã sửa:

- Banner tự fade out sau vài giây.

QA cần kiểm tra:

1. Vào Settings.
2. Lưu settings.
3. Quan sát banner `Đã lưu cấu hình.`.

Kỳ vọng:

- Banner hiển thị ngắn rồi tự ẩn, không che UI lâu.

### FIX-012 — Default Keep request logs tăng lên 1000

Đã khắc phục theo đề xuất:

- DEFAULT-001: `Keep request logs` mặc định quá thấp là `200`.

Nội dung đã sửa:

- Giá trị mặc định mới là `1000`.

QA cần kiểm tra:

1. Chạy môi trường với data/store mới hoặc reset settings.
2. Vào Settings.
3. Kiểm tra `Keep request logs` mặc định.

Kỳ vọng:

- Mặc định là `1000` đối với store/settings mới.
- Lưu ý: store cũ đã tồn tại có thể vẫn giữ giá trị cũ nếu người dùng từng lưu settings trước đó.

## 3. Lưu ý dữ liệu cũ

Một số request log cũ được tạo trước khi khắc phục có thể vẫn hiển thị token/cost bằng `0`. Đây là dữ liệu lịch sử, không phản ánh logic mới.

QA cần phân biệt:

- Log cũ trước thời điểm fix: có thể vẫn `0 tokens`.
- Log mới sau khi restart/chạy bản fix: phải có token ước lượng nếu request body có nội dung.

Khi báo lỗi lại, vui lòng ghi rõ request được tạo trước hay sau bản fix.

## 4. Hạng mục chưa coi là bug đã fix hoàn toàn

### Test model timeout/cancel

Báo cáo có BUG-006 về test model mất khoảng 19 giây, chưa có cancel/progress timer. Bản khắc phục hiện tại đã giữ spinner/trạng thái testing hiện có, nhưng chưa triển khai nút Cancel và elapsed timer riêng.

QA vui lòng kiểm tra lại:

- Nếu test model vẫn quá lâu, ghi rõ provider/model/thời gian thực tế.
- Đề xuất timeout hợp lý cho từng provider nếu có.

## 5. Mẫu phản hồi retest

QA vui lòng phản hồi theo mẫu sau cho từng mục:

```text
Mã retest: FIX-001 / FIX-002 / ...
Kết quả: Pass / Fail / Partial
Môi trường: Chrome/Edge/Firefox, desktop/mobile, độ phân giải
Dữ liệu test: provider, model, API key policy, pricing rule nếu có
Các bước đã kiểm tra:
1. ...
2. ...
3. ...
Kết quả thực tế:
Kết quả mong muốn nếu Fail/Partial:
Ảnh chụp/video/log console/network:
Gợi ý bổ sung:
```

## 6. Kết quả build/test nội bộ trước khi gửi QA

Đã kiểm tra nội bộ:

```text
gofmt: pass
go test ./...: pass
go build ./cmd/vivurouter-go: pass
```
