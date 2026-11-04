package main

import (
	"strings"
	"time"

	"github.com/TanNhatCMS/zcode-popup-watcher/internal/checker"
)

// pollCallbacks là những điểm khác nhau giữa 3 chế độ chạy
// (check console, daemon non-Windows, app tray Windows).
// runPoll giữ phần chung: gọi API, backoff, lọc deal mới, lưu seen.
type pollCallbacks struct {
	// log nhận dòng log (chưa có prefix); từng chế độ tự thêm
	// prefix riêng ([checker], [tray-child], [gotray]) và in/log.
	log func(format string, args ...any)
	// onError chạy thêm khi query lỗi (tray cập nhật tooltip;
	// check/daemon để trống).
	onError func(err error)
	// onSuccess chạy sau mỗi lần query thành công (tray cập nhật
	// tooltip "check cuối"; check/daemon để trống).
	onSuccess func(deals, fresh []checker.Deal)
	// onFresh chạy khi có deal mới (notify, ghi notify file,
	// cập nhật icon/menu tray...).
	onFresh func(fresh []checker.Deal, message string)
	// waitInterval chờ giữa 2 lần poll; false = dừng vòng lặp
	// (tray thoát).
	waitInterval func(d time.Duration) bool
	// waitError chờ sau lỗi; false = dừng vòng lặp (tray thoát).
	waitError func(d time.Duration) bool
}

// runPoll chạy vòng poll cho tới khi once xong hoặc wait trả false.
// Trả 0 khi dừng bình thường, 1 khi once gặp lỗi.
func runPoll(token, deviceMid, locale, appVersion string, interval time.Duration, once bool, cb pollCallbacks) int {
	seq := 0
	failures := 0
	seen := checker.LoadSeen()
	for {
		deals, err := checker.QueryDeals(token, deviceMid, locale, appVersion, seq)
		seq++
		if err != nil {
			failures++
			delay := 30 * time.Second * time.Duration(1<<min(failures-1, 5))
			if delay > 10*time.Minute {
				delay = 10 * time.Minute
			}
			cb.log("Check lỗi (%v). Đợi %s.", err, delay)
			if cb.onError != nil {
				cb.onError(err)
			}
			if once {
				return 1
			}
			if !cb.waitError(delay) {
				return 0
			}
			continue
		}
		failures = 0
		var fresh []checker.Deal
		for _, deal := range deals {
			if !seen[deal.Key] {
				fresh = append(fresh, deal)
			}
		}
		keys := strings.Join(dealKeys(deals), ", ")
		if keys == "" {
			keys = "(rỗng)"
		}
		cb.log("Có %d delivery, %d mới: %s", len(deals), len(fresh), keys)
		if cb.onSuccess != nil {
			cb.onSuccess(deals, fresh)
		}
		if len(fresh) > 0 {
			for _, deal := range fresh {
				seen[deal.Key] = true
			}
			checker.SaveSeen(seen)
			cb.onFresh(fresh, formatDealsMessage(fresh))
		}
		if once {
			return 0
		}
		if !cb.waitInterval(interval) {
			return 0
		}
	}
}
