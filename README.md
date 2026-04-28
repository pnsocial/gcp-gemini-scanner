# gcp-gemini-scanner

CLI tool kiểm tra bảo mật cho Google Cloud Organization. Phát hiện các project đang bật Gemini API hoặc Vertex AI, liệt kê API Key đang hoạt động và hỗ trợ truy vết người tạo key qua Cloud Logging.

---

## Tính năng

- Quét toàn bộ project trong một Organization hoặc theo danh sách Folder
- Phát hiện project đang bật **Gemini API** (`generativelanguage.googleapis.com`) và **Vertex AI** (`aiplatform.googleapis.com`)
- Liệt kê API Key đang **ACTIVE** có quyền truy cập Gemini / Vertex AI
- Sinh **Audit URL** trỏ thẳng đến sự kiện tạo key trong Cloud Logging
- Xuất kết quả ra file **CSV** và hiển thị bảng tóm tắt tại terminal
- Hỗ trợ **Dry-run** để xem danh sách project trước khi scan

---

## Yêu cầu

### Môi trường

- Go 1.21+
- Đã cài [Google Cloud SDK](https://cloud.google.com/sdk/docs/install) và đăng nhập ADC:

```bash
gcloud auth application-default login
```

### Quyền IAM tối thiểu

| Permission | Mục đích |
|------------|----------|
| `resourcemanager.projects.list` | Duyệt project trong Org / Folder |
| `resourcemanager.folders.list` | Duyệt cây folder |
| `serviceusage.services.list` | Kiểm tra trạng thái API |
| `apikeys.keys.list` | Liệt kê API Key |

> Tool sẽ tự động bỏ qua các project hoặc folder không có quyền truy cập và tiếp tục quét phần còn lại.

---

## Cài đặt

```bash
git clone https://github.com/your-org/gcp-gemini-scanner.git
cd gcp-gemini-scanner
go build -o gcp-gemini-scanner ./cmd/gemini-api-scanner
```

---

## Sử dụng

### Quét theo Organization

```bash
./gcp-gemini-scanner --orgid 123456789
```

### Quét theo Folder

```bash
./gcp-gemini-scanner --folderid 111111111,222222222
```

### Tùy chọn phổ biến

```bash
./gcp-gemini-scanner --orgid 123456789 \
  --output results.csv \
  --rps 30 \
  --workers 10 \
  --dry-run
```

### Toàn bộ flags

| Flag | Mặc định | Mô tả |
|------|----------|-------|
| `--orgid` | — | Organization ID |
| `--folderid` | — | Folder ID, hỗ trợ nhiều ID cách nhau dấu phẩy |
| `--output` | `scan_results.csv` | Đường dẫn file CSV đầu ra |
| `--workers` | `10` | Số goroutine xử lý song song |
| `--rps` | `50` | Giới hạn request/giây để tránh vượt quota |
| `--max-depth` | `20` | Giới hạn độ sâu khi duyệt cây folder |
| `--dry-run` | `false` | Chỉ liệt kê project, không scan API |
| `--debug` | `false` | Hiển thị log chi tiết |

---

## Output

### Terminal

```
[✔] Xác thực thông tin (ADC)
[✔] Tiếp nhận và rà soát Projects        (127 projects)
[✔] Kiểm tra API Endpoints               (127 / 127 projects)
[✔] Tổng hợp và xuất báo cáo

─────────────────────────────────────────
Hoàn tất. Thời gian: 2m14s
Projects đã quét : 127
Có Gemini/Vertex  : 34
API Keys tìm thấy : 58
Lỗi / Bị bỏ qua  : 3 projects (xem stderr)
Kết quả lưu tại  : scan_results.csv
─────────────────────────────────────────

+----------------+--------+--------+---------------+-----------+-------------+----------------------+
| PROJECT ID     | GEMINI | VERTEX | KEY           | KEY TYPE  | RESTRICTION | CREATED (UTC)        |
+----------------+--------+--------+---------------+-----------+-------------+----------------------+
| proj-alpha-123 | ✔      | ✔      | my-gemini-key | API_KEY   | NONE        | 2024-11-01T08:00:00Z |
| proj-beta-456  | ✔      | ✘      | analytics-key | API_KEY   | RESTRICTED  | 2024-12-15T14:22:00Z |
+----------------+--------+--------+---------------+-----------+-------------+----------------------+
```

### CSV

File CSV chứa đầy đủ thông tin bao gồm: Organization, Full Folder Path, Project Name, Project ID, Gemini Service Status, Vertex Service Status, Key Display Name, Key UID, Restriction Type, Created Time (UTC), và Audit URL trỏ đến Cloud Logging.

---

## Lưu ý

- Tool chỉ thu thập **metadata** của API Key (tên, UID, thời gian tạo). Không truy cập hoặc hiển thị giá trị bí mật của key.
- Nếu bị ngắt giữa chừng (Ctrl+C), dữ liệu đã quét được sẽ được lưu xuống file CSV trước khi thoát.
