# Changelog QA fixes

## 2026-06-08

### Pricing/cost

- Dashboard Cost giờ có note/tooltip phân biệt các trường hợp:
  - chưa có token để tính cost;
  - có token nhưng chưa có pricing rule;
  - đã có pricing/custom pricing.
- Nếu request có token nhưng không match pricing rule, cost vẫn là `$0` nhưng UI hiển thị rõ là `chưa có pricing rule`, không im lặng như trước.

### Providers

- Badge default được đổi theo loại provider:
  - `Default OpenAI` cho provider mặc định của `/v1/chat/completions`.
  - `Default Codex` cho provider mặc định của `/v1/responses` và `/codex/responses`.
- Badge row dùng flex-wrap/gap đồng nhất để tránh `Direct IP` rơi xuống hàng nhìn như lỗi layout.
- Credentials tooltip hiển thị ý nghĩa an toàn như `access_token (OAuth, đã lưu)`, không lộ secret thật.

### Requests

- Tooltip timestamp của cột Time được chuyển sang local time bằng JavaScript khi browser render.

### Test model

- Backend `/api/providers/test-model` dùng timeout mặc định 15 giây.
- Timeout trả lỗi rõ ràng `upstream timeout after 15s`.
- Frontend hiển thị elapsed counter dạng `Testing... 3s`.
- Nút `Cancel` xuất hiện sau 3 giây và hủy request bằng `AbortController`.

### Settings / API keys

- Nút Copy Local API key có feedback `Copied ✓` trong 2 giây.
- Nếu Clipboard API bị từ chối, nút hiển thị `Không thể copy` và tự select input để người dùng copy thủ công.
- Thêm nút `Regenerate` cho Local API key với confirm trước khi sinh key mới.
- API key card có toggle `Enable/Disable`; key disabled vẫn lưu trong settings nhưng không được gateway chấp nhận.
- Bật `Require API key` khi chưa có API key policy sẽ hiện cảnh báo inline.

### Keep request logs

- Giá trị mặc định mới là `1000` cho store/settings mới.
- Store cũ có thể vẫn giữ giá trị đã lưu trước đó; người dùng có thể chỉnh trực tiếp trong Settings.
